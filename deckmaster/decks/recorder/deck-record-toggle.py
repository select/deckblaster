#!/usr/bin/env python3
"""
Toggle deck recording. Instant stop + async encode with status display.
Writes status icons to /tmp/streamdeck-recording/status-icon.png
which the icon_command picks up automatically.
"""

import json
import os
import signal
import subprocess
import sys
import time

PID_FILE = "/tmp/streamdeck-recording/recorder.pid"
STATE_FILE = "/tmp/streamdeck-recording/state.json"
STATUS_ICON = "/tmp/streamdeck-recording/status-icon.png"
FRAMES_DIR = "/tmp/streamdeck-recording/frames"
VIDEO_DIR = os.path.expanduser("~/Videos")
SCALE = 3


def is_recording():
    if os.path.exists(PID_FILE):
        try:
            pid = int(open(PID_FILE).read().strip())
            os.kill(pid, 0)
            return True
        except (ProcessLookupError, ValueError):
            pass
    return False


def render_status(text, color=(200, 200, 200), bg=(0, 0, 0)):
    """Render a status text icon to the status-icon path."""
    from PIL import Image, ImageDraw, ImageFont
    SIZE = 72
    img = Image.new("RGBA", (SIZE, SIZE), (*bg, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([3, 3, 69, 69], radius=8, outline=(60, 60, 80), width=1)

    try:
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
    except Exception:
        font = ImageFont.load_default()

    lines = []
    for word in text.split():
        if lines:
            test = lines[-1] + " " + word
            bb = draw.textbbox((0, 0), test, font=font)
            if bb[2] - bb[0] <= 62:
                lines[-1] = test
                continue
        lines.append(word)

    total_h = len(lines) * 14
    y = (SIZE - total_h) // 2
    for line in lines:
        bb = draw.textbbox((0, 0), line, font=font)
        tw = bb[2] - bb[0]
        draw.text(((SIZE - tw) // 2, y), line, fill=color, font=font)
        y += 14

    img.save(STATUS_ICON)


def start_recording():
    os.makedirs(FRAMES_DIR, exist_ok=True)
    os.makedirs(VIDEO_DIR, exist_ok=True)

    # Remove old status icon
    if os.path.exists(STATUS_ICON):
        os.remove(STATUS_ICON)

    # Clean old frames
    for f in os.listdir(FRAMES_DIR):
        os.remove(os.path.join(FRAMES_DIR, f))

    fps = 10
    state = {"fps": fps, "started": time.time(), "frame_count": 0}
    with open(STATE_FILE, "w") as f:
        json.dump(state, f)

    # Fork recorder
    pid = os.fork()
    if pid > 0:
        with open(PID_FILE, "w") as f:
            f.write(str(pid))
        return

    # Child — recorder loop
    os.setsid()
    import urllib.request
    interval = 1.0 / fps
    frame = 0

    def handle_stop(sig, _):
        sys.exit(0)
    signal.signal(signal.SIGTERM, handle_stop)

    while True:
        t0 = time.time()
        try:
            path = os.path.join(FRAMES_DIR, f"frame-{frame:06d}.png")
            urllib.request.urlretrieve("http://localhost:9990/screenshot", path)
            frame += 1
            with open(STATE_FILE, "w") as sf:
                json.dump({"fps": fps, "started": state["started"], "frame_count": frame}, sf)
        except Exception:
            pass
        elapsed = time.time() - t0
        if interval - elapsed > 0:
            time.sleep(interval - elapsed)


def stop_recording():
    # 1. Kill recorder immediately
    if os.path.exists(PID_FILE):
        try:
            pid = int(open(PID_FILE).read().strip())
            os.kill(pid, signal.SIGTERM)
        except (ProcessLookupError, ValueError):
            pass
        os.remove(PID_FILE)

    # 2. Show "Encoding..." immediately
    render_status("Encoding...", color=(255, 200, 50), bg=(30, 25, 0))

    # 3. Fork the encode so the button returns instantly
    pid = os.fork()
    if pid > 0:
        return  # parent returns to deckmaster immediately

    # Child — encode in background
    os.setsid()
    _do_encode()


def _do_encode():
    """Background encode process — composites keys onto Stream Deck frame."""
    state = {}
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            state = json.load(f)

    fps = state.get("fps", 5)
    frame_count = state.get("frame_count", 0)

    if frame_count == 0:
        render_status("No frames", color=(255, 80, 80))
        sys.exit(0)

    render_status("Compositing...", color=(255, 200, 50), bg=(30, 25, 0))

    from PIL import Image

    # Stream Deck photo template
    FRAME_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                              "..", "deckmaster", "decks", "assets", "recorder", "steamdeck-template.png")
    KEY_SRC = 72   # key size in screenshot
    GAP_SRC = 4    # gap in screenshot
    KEY_DST = 84   # key size in template (2x upscaled)
    V_GAP = 9      # vertical gap (tighter)
    X0, Y0 = 358, 309  # first key top-left (2x)
    GRID_RIGHT = 812   # right edge (2x)
    COLS, ROWS = 5, 3
    col_xs = [X0 + int(col * (GRID_RIGHT - X0 - KEY_DST) / 4) for col in range(COLS)]

    frame_img = Image.open(FRAME_PATH).convert("RGBA")
    frame_img = frame_img.resize((frame_img.width * 2, frame_img.height * 2), Image.LANCZOS)
    comp_dir = os.path.join(os.path.dirname(FRAMES_DIR), "composited")
    os.makedirs(comp_dir, exist_ok=True)

    for i in range(frame_count):
        src_path = os.path.join(FRAMES_DIR, f"frame-{i:06d}.png")
        if not os.path.exists(src_path):
            continue

        src = Image.open(src_path).convert("RGBA")
        comp = frame_img.copy()

        for row in range(ROWS):
            for col in range(COLS):
                # Extract key from screenshot
                sx = col * (KEY_SRC + GAP_SRC)
                sy = row * (KEY_SRC + GAP_SRC)
                key_img = src.crop((sx, sy, sx + KEY_SRC, sy + KEY_SRC))
                key_img = key_img.resize((KEY_DST, KEY_DST), Image.LANCZOS)

                # Paste onto frame
                dx = col_xs[col]
                dy = Y0 + row * (KEY_DST + V_GAP)
                comp.paste(key_img, (dx, dy))

        comp_path = os.path.join(comp_dir, f"frame-{i:06d}.png")
        comp.save(comp_path)

    render_status("Encoding...", color=(255, 200, 50), bg=(30, 25, 0))

    timestamp = time.strftime("%Y%m%d-%H%M%S")
    output = os.path.join(VIDEO_DIR, f"streamdeck-{timestamp}.mp4")

    cmd = [
        "ffmpeg", "-y",
        "-framerate", str(fps),
        "-i", os.path.join(comp_dir, "frame-%06d.png"),
        "-vf", "scale=iw*2:ih*2:flags=lanczos,pad=ceil(iw/2)*2:ceil(ih/2)*2",
        "-c:v", "libx264",
        "-pix_fmt", "yuv420p",
        "-crf", "18",
        output,
    ]

    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode == 0:
        basename = os.path.basename(output)
        name = basename.replace("streamdeck-", "").replace(".mp4", "")
        render_status(f"Saved! {name}", color=(80, 255, 120), bg=(10, 30, 10))
    else:
        render_status("Encode FAIL", color=(255, 60, 60))

    sys.exit(0)


if __name__ == "__main__":
    if is_recording():
        stop_recording()
    else:
        start_recording()
