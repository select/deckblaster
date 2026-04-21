#!/usr/bin/env python3
"""
Record Stream Deck screenshots as individual frames.

Usage:
    deck-record.py start [--fps N]   Start recording (default 5 fps)
    deck-record.py stop              Stop recording, render video
    deck-record.py status            Show recording status

Frames saved to: /tmp/streamdeck-recording/frames/
Video output:    ~/Videos/streamdeck-<timestamp>.mp4
"""

import json
import os
import signal
import subprocess
import sys
import time
import urllib.request

API = "http://localhost:9990/screenshot"
REC_DIR = "/tmp/streamdeck-recording"
FRAMES_DIR = os.path.join(REC_DIR, "frames")
STATE_FILE = os.path.join(REC_DIR, "state.json")
PID_FILE = os.path.join(REC_DIR, "recorder.pid")
VIDEO_DIR = os.path.expanduser("~/Videos")

SCALE = 3  # upscale for crisp video


def save_state(state):
    with open(STATE_FILE, "w") as f:
        json.dump(state, f)


def load_state():
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            return json.load(f)
    return {}


def cmd_start(fps=5):
    os.makedirs(FRAMES_DIR, exist_ok=True)
    os.makedirs(VIDEO_DIR, exist_ok=True)

    # Clean old frames
    for f in os.listdir(FRAMES_DIR):
        os.remove(os.path.join(FRAMES_DIR, f))

    state = {
        "fps": fps,
        "started": time.time(),
        "frame_count": 0,
    }
    save_state(state)

    # Fork recorder process
    pid = os.fork()
    if pid > 0:
        # Parent
        with open(PID_FILE, "w") as f:
            f.write(str(pid))
        print(f"Recording started (PID {pid}, {fps} fps)")
        print(f"Frames: {FRAMES_DIR}")
        return

    # Child — the recorder loop
    os.setsid()
    interval = 1.0 / fps
    frame = 0

    def handle_stop(sig, _):
        sys.exit(0)

    signal.signal(signal.SIGTERM, handle_stop)

    while True:
        t0 = time.time()
        try:
            path = os.path.join(FRAMES_DIR, f"frame-{frame:06d}.png")
            urllib.request.urlretrieve(API, path)
            frame += 1
            # Update count in state
            state = load_state()
            state["frame_count"] = frame
            save_state(state)
        except Exception:
            pass

        elapsed = time.time() - t0
        sleep_time = interval - elapsed
        if sleep_time > 0:
            time.sleep(sleep_time)


def cmd_stop():
    if not os.path.exists(PID_FILE):
        print("Not recording.")
        return

    pid = int(open(PID_FILE).read().strip())
    try:
        os.kill(pid, signal.SIGTERM)
        time.sleep(0.5)
    except ProcessLookupError:
        pass
    os.remove(PID_FILE)

    state = load_state()
    fps = state.get("fps", 5)
    frame_count = state.get("frame_count", 0)
    started = state.get("started", 0)
    duration = time.time() - started if started else 0

    print(f"Recording stopped: {frame_count} frames, {duration:.1f}s")

    if frame_count == 0:
        print("No frames captured.")
        return

    # Render video with ffmpeg
    timestamp = time.strftime("%Y%m%d-%H%M%S")
    output = os.path.join(VIDEO_DIR, f"streamdeck-{timestamp}.mp4")

    # Use ffmpeg: upscale with nearest-neighbor for pixel-crisp look
    cmd = [
        "ffmpeg", "-y",
        "-framerate", str(fps),
        "-i", os.path.join(FRAMES_DIR, "frame-%06d.png"),
        "-vf", f"scale=iw*{SCALE}:ih*{SCALE}:flags=neighbor",
        "-c:v", "libx264",
        "-pix_fmt", "yuv420p",
        "-crf", "18",
        output,
    ]

    print(f"Rendering video...")
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode == 0:
        print(f"Video saved: {output}")
    else:
        print(f"ffmpeg error: {result.stderr[-200:]}")


def cmd_status():
    if not os.path.exists(PID_FILE):
        print("Not recording.")
        return

    pid = int(open(PID_FILE).read().strip())
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        print("Not recording (stale PID).")
        return

    state = load_state()
    elapsed = time.time() - state.get("started", time.time())
    frames = state.get("frame_count", 0)
    print(f"Recording: {frames} frames, {elapsed:.0f}s elapsed")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    cmd = sys.argv[1]
    if cmd == "start":
        fps = 5
        if "--fps" in sys.argv:
            idx = sys.argv.index("--fps")
            fps = int(sys.argv[idx + 1])
        cmd_start(fps)
    elif cmd == "stop":
        cmd_stop()
    elif cmd == "status":
        cmd_status()
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)
