#!/bin/bash
# Toggle Zoom mute and update local state.
DISPLAY=:1
export DISPLAY
STATE_FILE="/tmp/streamdeck-zoom-state"
SCRIPT_DIR="$(dirname "$0")"

"$SCRIPT_DIR/zoom-send-key.sh" alt+a || exit 0

# Flip muted bit in state file
source "$STATE_FILE" 2>/dev/null || { echo "muted=0 video_off=0 hand_raised=0" > "$STATE_FILE"; source "$STATE_FILE"; }
NEW=$(( 1 - ${muted:-0} ))
sed -i "s/muted=[01]/muted=$NEW/" "$STATE_FILE"
