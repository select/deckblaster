#!/bin/bash
# Toggle Zoom raise hand and update local state.
DISPLAY=:1
export DISPLAY
STATE_FILE="/tmp/streamdeck-zoom-state"
SCRIPT_DIR="$(dirname "$0")"

"$SCRIPT_DIR/zoom-send-key.sh" alt+y || exit 0

source "$STATE_FILE" 2>/dev/null || { echo "muted=0 video_off=0 hand_raised=0" > "$STATE_FILE"; source "$STATE_FILE"; }
NEW=$(( 1 - ${hand_raised:-0} ))
sed -i "s/hand_raised=[01]/hand_raised=$NEW/" "$STATE_FILE"
