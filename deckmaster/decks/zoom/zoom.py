#!/usr/bin/env python3
"""Zoom Stream Deck controls.

Usage:
  zoom.py icon  mic|video|hand|leave  — print path to icon for that button
  zoom.py toggle mute|video|hand      — send keystroke + flip state bit
  zoom.py leave                       — send alt+q + clear state

State file: /tmp/streamdeck-zoom-state
  Single line: muted=0 video_off=0 hand_raised=0
  Reset automatically when no Zoom meeting window is found.
"""
import os
import sys
import subprocess
import re
from pathlib import Path

DISPLAY    = os.environ.get("DISPLAY", ":1")
STATE_FILE = Path("/tmp/streamdeck-zoom-state")
ASSETS     = Path(__file__).parent / "assets"

DEFAULT_STATE = {"muted": "0", "video_off": "0", "hand_raised": "0"}

# ── helpers ───────────────────────────────────────────────────────────────────

def in_meeting():
    r = subprocess.run(
        ["xdotool", "search", "--limit", "1", "--class", "zoom"],
        capture_output=True, env={**os.environ, "DISPLAY": DISPLAY},
    )
    return r.returncode == 0

def load_state():
    try:
        text = STATE_FILE.read_text()
        state = dict(DEFAULT_STATE)
        for m in re.finditer(r"(\w+)=([01])", text):
            state[m.group(1)] = m.group(2)
        return state
    except FileNotFoundError:
        return dict(DEFAULT_STATE)

def save_state(state):
    STATE_FILE.write_text(" ".join(f"{k}={v}" for k, v in state.items()) + "\n")

def send_key(key):
    subprocess.run(
        ["xdotool", "key", key],
        env={**os.environ, "DISPLAY": DISPLAY},
    )

# ── subcommands ───────────────────────────────────────────────────────────────

ICONS = {
    #  button   active-on          active-off          no-meeting
    "mic":   ("mic-on.png",    "mic-off.png",    "no-meeting-mic.png",   "muted"),
    "video": ("video-on.png",  "video-off.png",  "no-meeting-video.png", "video_off"),
    "hand":  ("hand-down.png", "hand-up.png",    "no-meeting-hand.png",  "hand_raised"),
    "leave": ("leave.png",     "leave.png",      "no-meeting-leave.png", None),
}

TOGGLES = {
    "mute":  ("alt+a", "muted"),
    "video": ("alt+v", "video_off"),
    "hand":  ("alt+y", "hand_raised"),
}

def cmd_icon(button):
    on, off, no_meeting, field = ICONS[button]
    if not in_meeting():
        print(ASSETS / no_meeting)
        return
    if field is None:
        print(ASSETS / on)
        return
    state = load_state()
    print(ASSETS / (off if state.get(field) == "1" else on))

def cmd_toggle(action):
    if action not in TOGGLES or not in_meeting():
        return
    key, field = TOGGLES[action]
    send_key(key)
    state = load_state()
    state[field] = "0" if state[field] == "1" else "1"
    save_state(state)

def cmd_leave():
    if not in_meeting():
        return
    send_key("alt+q")
    STATE_FILE.unlink(missing_ok=True)

# ── main ──────────────────────────────────────────────────────────────────────

def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    arg = sys.argv[2] if len(sys.argv) > 2 else ""

    if cmd == "icon" and arg in ICONS:
        cmd_icon(arg)
    elif cmd == "toggle" and arg in TOGGLES:
        cmd_toggle(arg)
    elif cmd == "leave":
        cmd_leave()
    else:
        print(f"usage: zoom.py icon mic|video|hand|leave | toggle mute|video|hand | leave",
              file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
