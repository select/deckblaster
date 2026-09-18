#!/usr/bin/env python3
"""Dynamic bottom row for main.deck (keys 10-14).

While an active Zoom meeting exists, the bottom row becomes Zoom controls;
otherwise it keeps switching virtual desktops. The decision is made per
icon poll and per key press, so it flips automatically when a meeting
starts/ends.

  slot 0 (key 10)  Mute / Unmute            Alt+A
  slot 1 (key 11)  Start / Stop video       Alt+V
  slot 2 (key 12)  Raise / Lower hand       Alt+Y
  slot 3 (key 13)  Start / Stop sharing     Alt+S
  slot 4 (key 14)  Leave meeting            Alt+Q (stops share first)

Usage:
  mainrow.py icon  <slot>   # print the icon path for a slot
  mainrow.py press <slot>   # run the action for a slot
"""
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import zoom  # noqa: E402

VDESKTOP = HERE.parent / "vdesktop" / "vdesktop-render.py"
DESK_IMG = "/tmp/streamdeck-vdesktop/desk-{}.png"
SLOTS = ("mute", "video", "hand", "share", "leave")
# slot action → zoom.icon_for() button name
ICON_BUTTON = {"mute": "mic", "video": "video", "hand": "hand", "leave": "leave"}


def icon(slot):
    action = SLOTS[slot]
    meeting = zoom.find_meeting_window()
    if not meeting:
        # Not in a meeting → show the virtual desktop button again.
        zoom.STATE_FILE.unlink(missing_ok=True)
        print(DESK_IMG.format(slot))
        return
    if action == "share":
        name = "share-stop.png" if zoom.is_sharing() else "share-start.png"
        print(zoom.ASSETS / name)
    else:
        print(zoom.icon_for(ICON_BUTTON[action], meeting))


def press(slot):
    action = SLOTS[slot]
    if not zoom.in_meeting():
        subprocess.run(["python3", str(VDESKTOP), "switch", str(slot)])
        return
    if action == "leave":
        zoom.cmd_leave()
    elif action == "share":
        zoom.cmd_share()
    else:
        zoom.cmd_toggle(action)


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    try:
        slot = int(sys.argv[2]) if len(sys.argv) > 2 else -1
    except ValueError:
        slot = -1
    if cmd not in ("icon", "press") or not (0 <= slot < len(SLOTS)):
        print("usage: mainrow.py icon|press <slot 0-4>", file=sys.stderr)
        sys.exit(1)
    if cmd == "icon":
        icon(slot)
    else:
        press(slot)


if __name__ == "__main__":
    main()