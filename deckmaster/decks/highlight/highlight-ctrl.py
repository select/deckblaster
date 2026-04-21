#!/usr/bin/env python3
"""
Mouse highlighter controller for Stream Deck.

State file: /tmp/streamdeck-highlight.json
Commands: init, toggle, size-up, size-down, color-next, color-prev, opacity-up, opacity-down
"""

import json
import os
import signal
import subprocess
import sys
import urllib.request
from PIL import Image, ImageDraw, ImageFont

# Add scripts dir to path for shared modules
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from wal_colors import load_wal_colors, hex_to_rgb

API = "http://localhost:9990"
ASSET = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
BIN = os.path.join(os.path.dirname(os.path.abspath(__file__)), "highlight-pointer")
STATE_FILE = os.path.expanduser("~/.local/share/streamdeck-highlight.json")
PID_FILE = "/tmp/streamdeck-highlight.pid"
TMP = "/tmp/streamdeck-highlight"
os.makedirs(TMP, exist_ok=True)
os.makedirs(ASSET, exist_ok=True)

SIZE = 72
KEY_STATUS = 0

RADII = [10, 15, 20, 30, 40, 50, 70]
OPACITIES = [0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]

try:
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 18)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 13)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 10)
except Exception:
    FONT_LG = FONT_MD = FONT_SM = ImageFont.load_default()


def load_state():
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            return json.load(f)
    return {"active": False, "radius_idx": 2, "color_idx": 0, "opacity_idx": 3}


def save_state(state):
    with open(STATE_FILE, "w") as f:
        json.dump(state, f)


def push_icon(key, path):
    data = json.dumps({"icon": path}).encode()
    req = urllib.request.Request(f"{API}/key/{key}", data=data,
                                headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=2)
    except Exception as e:
        print(f"API error key {key}: {e}", file=sys.stderr)


def is_running():
    if os.path.exists(PID_FILE):
        try:
            pid = int(open(PID_FILE).read().strip())
            os.kill(pid, 0)
            return True
        except (ValueError, ProcessLookupError, PermissionError):
            os.remove(PID_FILE)
    return False


def start_highlight(state):
    colors = load_wal_colors()
    color = colors[state["color_idx"] % len(colors)][0]
    opacity = OPACITIES[state["opacity_idx"]]
    radius_idx = state.get("radius_idx", 2)
    radius = RADII[min(radius_idx, len(RADII) - 1)]

    cmd = [
        BIN,
        "--released-color", color,
        "--pressed-color", "#ffffff",
        "--radius", str(radius),
        "--opacity", str(opacity),
        "--outline", "0",
        "--show-cursor",
    ]

    proc = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            env={**os.environ, "DISPLAY": ":1"})
    with open(PID_FILE, "w") as f:
        f.write(str(proc.pid))

    state["active"] = True
    save_state(state)


def stop_highlight(state):
    if os.path.exists(PID_FILE):
        try:
            pid = int(open(PID_FILE).read().strip())
            os.kill(pid, signal.SIGTERM)
        except (ValueError, ProcessLookupError):
            pass
        os.remove(PID_FILE)
    subprocess.run(["killall", "highlight-pointer"], capture_output=True)
    state["active"] = False
    save_state(state)


def render_status(state):
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)

    active = state.get("active", False) and is_running()
    colors = load_wal_colors()
    color_hex = colors[state.get("color_idx", 0) % len(colors)][0]
    r, g, b = hex_to_rgb(color_hex)

    if active:
        draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(15, 30, 15), outline=(0, 180, 0), width=2)
    else:
        draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(25, 10, 10), outline=(120, 40, 40), width=2)

    status = "ON" if active else "OFF"
    status_color = (0, 255, 80) if active else (255, 60, 60)
    draw.text((20, 4), status, fill=status_color, font=FONT_LG)

    radius_idx = state.get("radius_idx", 2)
    preview_r = min(RADII[min(radius_idx, len(RADII) - 1)] // 2, 20)
    cx, cy = 36, 42
    draw.ellipse([cx - preview_r, cy - preview_r, cx + preview_r, cy + preview_r],
                 fill=(r, g, b, int(OPACITIES[state.get("opacity_idx", 3)] * 255)))

    radius_val = RADII[min(radius_idx, len(RADII) - 1)]
    draw.text((8, 58), f"r={radius_val}", fill=(180, 180, 180), font=FONT_SM)

    path = os.path.join(TMP, "status.png")
    img.save(path)
    return path


def update_display(state):
    path = render_status(state)
    push_icon(KEY_STATUS, path)


# ── Commands ────────────────────────────────────────────────────────────────────

def cmd_init():
    state = load_state()
    state["active"] = is_running()
    save_state(state)
    update_display(state)


def cmd_toggle():
    state = load_state()
    if is_running():
        stop_highlight(state)
    else:
        start_highlight(state)
    update_display(state)


def cmd_size_up():
    state = load_state()
    idx = state.get("radius_idx", 2)
    state["radius_idx"] = min(idx + 1, len(RADII) - 1)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


def cmd_size_down():
    state = load_state()
    idx = state.get("radius_idx", 2)
    state["radius_idx"] = max(idx - 1, 0)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


def cmd_color_next():
    state = load_state()
    colors = load_wal_colors()
    idx = state.get("color_idx", 0)
    state["color_idx"] = (idx + 1) % len(colors)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


def cmd_color_prev():
    state = load_state()
    colors = load_wal_colors()
    idx = state.get("color_idx", 0)
    state["color_idx"] = (idx - 1) % len(colors)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


def cmd_opacity_up():
    state = load_state()
    idx = state.get("opacity_idx", 3)
    state["opacity_idx"] = min(idx + 1, len(OPACITIES) - 1)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


def cmd_opacity_down():
    state = load_state()
    idx = state.get("opacity_idx", 3)
    state["opacity_idx"] = max(idx - 1, 0)
    save_state(state)
    if is_running():
        stop_highlight(state)
        start_highlight(state)
    update_display(state)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: highlight-ctrl.py {init|toggle|size-up|size-down|color-next|color-prev|opacity-up|opacity-down}")
        sys.exit(1)

    cmds = {
        "init": cmd_init,
        "toggle": cmd_toggle,
        "size-up": cmd_size_up,
        "size-down": cmd_size_down,
        "color-next": cmd_color_next,
        "color-prev": cmd_color_prev,
        "opacity-up": cmd_opacity_up,
        "opacity-down": cmd_opacity_down,
    }
    fn = cmds.get(sys.argv[1])
    if fn:
        fn()
    else:
        print(f"Unknown: {sys.argv[1]}", file=sys.stderr)
        sys.exit(1)
