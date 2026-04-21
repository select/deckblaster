#!/bin/bash
# Returns current Zoom state as JSON-ish state file and maintains it.
# State file: /tmp/streamdeck-zoom-state
#   Fields: muted (0/1), video_off (0/1), hand_raised (0/1)
# State resets when no Zoom meeting window is found.

STATE_FILE="/tmp/streamdeck-zoom-state"
DISPLAY=:1
export DISPLAY

in_meeting() {
    xdotool search --limit 1 --class zoom --name "Zoom Meeting" &>/dev/null
}

# Reset state if not in a meeting
if ! in_meeting; then
    rm -f "$STATE_FILE"
    exit 0
fi

# Initialize state file if missing (joined a new meeting — assume unmuted, video on)
if [[ ! -f "$STATE_FILE" ]]; then
    echo "muted=0 video_off=0 hand_raised=0" > "$STATE_FILE"
fi
