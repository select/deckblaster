#!/usr/bin/env python3
"""Zoom Stream Deck controls (Hyprland / Wayland only).

Usage:
  zoom.py icon  mic|video|hand|leave  — print path to icon for that button
  zoom.py toggle mute|video|hand      — send keystroke + flip state bit
  zoom.py leave                       — stop sharing (if any) + send alt+q

Wayland only delivers keyboard input to the focused client, and Zoom (like
many apps, see hyprwm/Hyprland#6576) ignores synthetic shortcuts while it is
not focused. So each shortcut briefly focuses the Zoom meeting window, sends
the key, then restores the previous focus. On a shared workspace this is
seamless; if Zoom lives on another workspace the workspace flips across and
back.

While screen sharing Zoom replaces the meeting window with the floating
controls toolbar ("as_toolbar"), which ignores Alt+Q, so leave stops the share
first and waits for the meeting window to return.

Zoom exposes no readable live state (no MPRIS / D-Bus controls / AT-SPI / not
reflected in PipeWire), so mute/video/hand are tracked locally. State is reset
whenever the meeting window changes or disappears.

State file: /tmp/streamdeck-zoom-state
  Single line: muted=0 video_off=0 hand_raised=0 meeting=0x...
"""
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

STATE_FILE = Path("/tmp/streamdeck-zoom-state")
ASSETS = Path(__file__).parent / "assets"

DEFAULT_STATE = {"muted": "0", "video_off": "0", "hand_raised": "0"}

# Zoom toplevel titles that are not an active meeting (main client hub, etc.).
# Anything else with class "Zoom" is treated as a meeting window.
NON_MEETING_TITLE_MARKERS = (
    "zoom workplace",
    "zoom cloud meetings",
    "settings",
)

# Pause after a focus change so the compositor/clients settle before the key.
FOCUS_SETTLE = 0.06

# ── Hyprland helpers ──────────────────────────────────────────────────────────

def _hyprctl(*args):
    try:
        return subprocess.run(
            ["hyprctl", *args],
            capture_output=True, text=True, env=os.environ,
        )
    except FileNotFoundError:
        return None


def hyprctl_clients():
    """Return the list of Hyprland clients, or [] if Hyprland is unreachable."""
    r = _hyprctl("clients", "-j")
    if not r or r.returncode != 0 or not r.stdout.strip():
        return []
    try:
        return json.loads(r.stdout)
    except json.JSONDecodeError:
        return []


def active_window_address():
    r = _hyprctl("activewindow", "-j")
    if not r or r.returncode != 0 or not r.stdout.strip():
        return None
    try:
        return json.loads(r.stdout).get("address")
    except json.JSONDecodeError:
        return None


def find_meeting_window():
    """Return the address of the Zoom meeting window, or None if not in a meeting."""
    candidates = []
    for c in hyprctl_clients():
        if c.get("class", "").lower() != "zoom":
            continue
        if not c.get("mapped", True):
            continue
        title = c.get("title", "").lower()
        if any(marker in title for marker in NON_MEETING_TITLE_MARKERS):
            continue
        candidates.append(c)
    # Prefer a window explicitly titled as a meeting (there may be chat/popups).
    for c in candidates:
        if "meeting" in c.get("title", "").lower():
            return c.get("address")
    return candidates[0].get("address") if candidates else None


def in_meeting():
    """True when an active Zoom meeting window exists (hub alone does not count)."""
    return find_meeting_window() is not None


def is_sharing():
    """True while Zoom is screen sharing (meeting window replaced by as_toolbar)."""
    for c in hyprctl_clients():
        if c.get("class", "").lower() != "zoom":
            continue
        if "as_toolbar" in c.get("title", "").lower():
            return True
    return False


def wait_for_meeting_window(timeout=3.0):
    """Poll for the real meeting window (not the hub or the sharing toolbar)."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        for c in hyprctl_clients():
            if c.get("class", "").lower() != "zoom" or not c.get("mapped", True):
                continue
            title = c.get("title", "").lower()
            if any(m in title for m in NON_MEETING_TITLE_MARKERS) or "as_toolbar" in title:
                continue
            return c.get("address")
        time.sleep(0.1)
    return find_meeting_window()


def _focus(address):
    if address:
        _hyprctl("dispatch", f'hl.dsp.focus({{window="address:{address}"}})')



def _send_shortcut(mods, key, address):
    _hyprctl(
        "dispatch",
        f'hl.dsp.send_shortcut({{mods="{mods}", key="{key}", '
        f'window="address:{address}"}})',
    )


def send_shortcut(mods, key, meeting=None):
    """Send mods+key to the Zoom meeting window, focusing it just long enough."""
    meeting = meeting or find_meeting_window()
    if not meeting:
        return
    previous = active_window_address()
    if previous and previous.lower() == meeting.lower():
        # Already focused — just send.
        _send_shortcut(mods, key, meeting)
        return
    _focus(meeting)
    time.sleep(FOCUS_SETTLE)
    _send_shortcut(mods, key, meeting)
    time.sleep(FOCUS_SETTLE)
    _focus(previous)


# ── state helpers ─────────────────────────────────────────────────────────────

def load_state(meeting=None):
    """Read toggle state, resetting it if the meeting window has changed."""
    raw = {}
    try:
        raw = dict(re.findall(r"(\w+)=(\S+)", STATE_FILE.read_text()))
    except FileNotFoundError:
        pass

    state = dict(DEFAULT_STATE)
    state.update({k: v for k, v in raw.items() if k in DEFAULT_STATE})

    # New meeting (different window address) → forget previous toggle state.
    if meeting and raw.get("meeting") and raw["meeting"] != meeting:
        for key in DEFAULT_STATE:
            state[key] = "0"
    if meeting:
        state["meeting"] = meeting
    return state


def save_state(state):
    STATE_FILE.write_text(" ".join(f"{k}={v}" for k, v in state.items()) + "\n")


# ── subcommands ───────────────────────────────────────────────────────────────

ICONS = {
    #  button   active-on          active-off          no-meeting              state field
    "mic":   ("mic-on.png",    "mic-off.png",    "no-meeting-mic.png",   "muted"),
    "video": ("video-on.png",  "video-off.png",  "no-meeting-video.png", "video_off"),
    "hand":  ("hand-down.png", "hand-up.png",    "no-meeting-hand.png",  "hand_raised"),
    "leave": ("leave.png",     "leave.png",      "no-meeting-leave.png", None),
}

# action  → (mods, key, state field)
TOGGLES = {
    "mute":  ("ALT", "a", "muted"),
    "video": ("ALT", "v", "video_off"),
    "hand":  ("ALT", "y", "hand_raised"),
}


def icon_for(button, meeting):
    """Return the icon Path for a zoom button given the current meeting/state."""
    on, off, no_meeting, field = ICONS[button]
    if not meeting:
        return ASSETS / no_meeting
    if field is None:
        return ASSETS / on
    state = load_state(meeting)
    return ASSETS / (off if state.get(field) == "1" else on)


def cmd_icon(button):
    meeting = find_meeting_window()
    if not meeting:
        # Meeting window gone → forget stale toggle state.
        STATE_FILE.unlink(missing_ok=True)
    print(icon_for(button, meeting))


def cmd_toggle(action):
    meeting = find_meeting_window()
    if action not in TOGGLES or not meeting:
        return
    mods, key, field = TOGGLES[action]
    send_shortcut(mods, key, meeting)
    state = load_state(meeting)
    state[field] = "0" if state[field] == "1" else "1"
    save_state(state)


def cmd_share():
    """Toggle screen sharing (Alt+S)."""
    meeting = find_meeting_window()
    if meeting:
        send_shortcut("ALT", "s", meeting)


def cmd_leave():
    meeting = find_meeting_window()
    if not meeting:
        return
    # While sharing, the meeting window is replaced by the floating-controls
    # toolbar ("as_toolbar"), which ignores Alt+Q. Stop the share first so the
    # meeting window comes back, then leave.
    if is_sharing():
        send_shortcut("ALT", "s", meeting)
        meeting = wait_for_meeting_window()
    if meeting:
        send_shortcut("ALT", "q", meeting)
    STATE_FILE.unlink(missing_ok=True)


# ── main ──────────────────────────────────────────────────────────────────────

def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    arg = sys.argv[2] if len(sys.argv) > 2 else ""

    if cmd == "icon" and arg in ICONS:
        cmd_icon(arg)
    elif cmd == "toggle" and arg in TOGGLES:
        cmd_toggle(arg)
    elif cmd == "share":
        cmd_share()
    elif cmd == "leave":
        cmd_leave()
    else:
        print("usage: zoom.py icon mic|video|hand|leave | toggle mute|video|hand | share | leave",
              file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()