#!/bin/bash
# Send a keystroke to whichever window currently has focus.
# The Stream Deck doesn't steal focus, so Zoom stays active when you press a button.
DISPLAY=:1; export DISPLAY
KEY=${1:?usage: zoom-send-key.sh <key>}
xdotool key "$KEY"
