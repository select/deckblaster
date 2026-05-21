#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Mouse highlighter controller for Stream Deck.

Supports two backends:
  - X11: highlight-pointer binary (swillner/highlight-pointer)
  - Wayland: hati GNOME Shell extension (szymonwilczek/hati) via gsettings

Backend is auto-detected from XDG_SESSION_TYPE.

State file: ~/.local/share/streamdeck-highlight.json
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

# Hati size mapping: RADII -> hati "size" (diameter in px, range 40-200)
# highlight-pointer radius is half-diameter, hati size is full diameter
HATI_SIZES = [40, 50, 60, 80, 100, 130, 160]

HATI_SCHEMA = "org.gnome.shell.extensions.hati"
HATI_UUID = "hati@szymonwilczek.github.io"
HATI_SCHEMA_DIR = os.path.expanduser(
    "~/.local/share/gnome-shell/extensions/hati@szymonwilczek.github.io/schemas/"
)


def detect_backend():
    """Return 'wayland' or 'x11' based on session type."""
    session = os.environ.get("XDG_SESSION_TYPE", "x11").lower()
    if "wayland" in session:
        return "wayland"
    return "x11"


BACKEND = detect_backend()

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
    """Check if the highlighter is currently active."""
    if BACKEND == "wayland":
        return _hati_is_enabled()
    # X11: check PID file
    if os.path.exists(PID_FILE):
        try:
            pid = int(open(PID_FILE).read().strip())
            os.kill(pid, 0)
            return True
        except (ValueError, ProcessLookupError, PermissionError):
            os.remove(PID_FILE)
    return False


# ── Hati (Wayland) helpers ──────────────────────────────────────────────────────

def _hati_gsettings_get(key):
    try:
        env = {**os.environ, "GSETTINGS_SCHEMA_DIR": HATI_SCHEMA_DIR}
        result = subprocess.run(
            ["gsettings", "get", HATI_SCHEMA, key],
            capture_output=True, text=True, timeout=3, env=env)
        return result.stdout.strip()
    except Exception:
        return ""


def _hati_gsettings_set(key, value):
    try:
        env = {**os.environ, "GSETTINGS_SCHEMA_DIR": HATI_SCHEMA_DIR}
        subprocess.run(
            ["gsettings", "set", HATI_SCHEMA, key, value],
            capture_output=True, timeout=3, env=env)
    except Exception:
        pass


def _hati_is_enabled():
    return _hati_gsettings_get("enabled") == "true"


def _hati_enable():
    # Ensure extension is enabled in GNOME
    subprocess.run(["gnome-extensions", "enable", HATI_UUID],
                   capture_output=True, timeout=5)
    _hati_gsettings_set("enabled", "true")


def _hati_disable():
    _hati_gsettings_set("enabled", "false")


# Modes: "ring+click" (full), "click-only" (no ring, just click ripple), "off"
MODES = ["ring+click", "click-only", "off"]


def _hati_apply_settings(state):
    """Push current state to hati gsettings."""
    colors = load_wal_colors()
    color_hex = colors[state.get("color_idx", 0) % len(colors)][0]
    r, g, b = hex_to_rgb(color_hex)
    opacity = OPACITIES[state.get("opacity_idx", 3)]
    radius_idx = state.get("radius_idx", 2)
    hati_size = HATI_SIZES[min(radius_idx, len(HATI_SIZES) - 1)]
    mode = state.get("mode", "ring+click")

    # Shape always circle
    _hati_gsettings_set("shape", "'circle'")
    _hati_gsettings_set("corner-radius", "50")

    rgba_str = f"rgba({r}, {g}, {b}, {opacity:.1f})"

    if mode == "click-only":
        # Hide ring: transparent color, but keep size for click animation
        _hati_gsettings_set("color", "'rgba(0, 0, 0, 0.0)'")
        _hati_gsettings_set("opacity", "0.0")
        _hati_gsettings_set("glow", "false")
        _hati_gsettings_set("size", str(hati_size))  # keep size for click ripple
        _hati_gsettings_set("border-weight", "6")
    else:
        # Full ring + click
        _hati_gsettings_set("color", f"'{rgba_str}'")
        _hati_gsettings_set("size", str(hati_size))
        _hati_gsettings_set("opacity", str(opacity))
        _hati_gsettings_set("glow", "true")
        _hati_gsettings_set("border-weight", "4")

    # Click ripple effect (always on when hati is enabled)
    # Click colors always use full opacity regardless of ring opacity
    click_rgba = f"rgba({r}, {g}, {b}, 1.0)"
    _hati_gsettings_set("click-animations", "true")
    if mode == "click-only":
        _hati_gsettings_set("click-animation-mode", "'ring-expand'")
    else:
        _hati_gsettings_set("click-animation-mode", "'ripple'")
    _hati_gsettings_set("left-click-color", f"'{click_rgba}'")
    _hati_gsettings_set("right-click-color", f"'rgba({r}, {g}, {b}, 0.7)'")
    # Disable the built-in locate-pointer (Ctrl) since we have hati
    subprocess.run(["gsettings", "set", "org.gnome.desktop.interface",
                    "locate-pointer", "false"], capture_output=True)


def start_highlight(state):
    if BACKEND == "wayland":
        _hati_apply_settings(state)
        _hati_enable()
    else:
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
    if BACKEND == "wayland":
        _hati_disable()
    else:
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
    mode = state.get("mode", "off") if BACKEND == "wayland" else None
    colors = load_wal_colors()
    color_hex = colors[state.get("color_idx", 0) % len(colors)][0]
    r, g, b = hex_to_rgb(color_hex)

    if active:
        draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(15, 30, 15), outline=(0, 180, 0), width=2)
    else:
        draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(25, 10, 10), outline=(120, 40, 40), width=2)

    # Status text
    if not active:
        status = "OFF"
        status_color = (255, 60, 60)
    elif mode == "click-only":
        status = "CLICK"
        status_color = (100, 200, 255)
    else:
        status = "ON"
        status_color = (0, 255, 80)
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


def cmd_mode():
    """Cycle mode: ring+click → click-only → ring+click."""
    state = load_state()
    mode = state.get("mode", "ring+click")
    if mode == "ring+click":
        state["mode"] = "click-only"
    else:
        state["mode"] = "ring+click"
    save_state(state)
    if is_running() and BACKEND == "wayland":
        _hati_apply_settings(state)
    update_display(state)


def cmd_size_up():
    state = load_state()
    idx = state.get("radius_idx", 2)
    state["radius_idx"] = min(idx + 1, len(RADII) - 1)
    save_state(state)
    if is_running():
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
            stop_highlight(state)
            start_highlight(state)
    update_display(state)


def cmd_size_down():
    state = load_state()
    idx = state.get("radius_idx", 2)
    state["radius_idx"] = max(idx - 1, 0)
    save_state(state)
    if is_running():
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
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
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
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
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
            stop_highlight(state)
            start_highlight(state)
    update_display(state)


def cmd_opacity_up():
    state = load_state()
    idx = state.get("opacity_idx", 3)
    state["opacity_idx"] = min(idx + 1, len(OPACITIES) - 1)
    save_state(state)
    if is_running():
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
            stop_highlight(state)
            start_highlight(state)
    update_display(state)


def cmd_opacity_down():
    state = load_state()
    idx = state.get("opacity_idx", 3)
    state["opacity_idx"] = max(idx - 1, 0)
    save_state(state)
    if is_running():
        if BACKEND == "wayland":
            _hati_apply_settings(state)
        else:
            stop_highlight(state)
            start_highlight(state)
    update_display(state)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: highlight-ctrl.py {init|toggle|mode|size-up|size-down|color-next|color-prev|opacity-up|opacity-down}")
        sys.exit(1)

    cmds = {
        "init": cmd_init,
        "toggle": cmd_toggle,
        "mode": cmd_mode,
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
