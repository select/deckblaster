#!/bin/bash
# Return the appropriate record button icon based on recording state.
STATE="/tmp/streamdeck-recording"

# Check for status override (encoding/saved message)
if [ -f "$STATE/status-icon.png" ]; then
    # Show status icon, auto-expire after 10s
    age=$(( $(date +%s) - $(stat -c %Y "$STATE/status-icon.png") ))
    if [ "$age" -lt 10 ]; then
        echo "$STATE/status-icon.png"
        exit 0
    else
        rm -f "$STATE/status-icon.png"
    fi
fi

if [ -f "$STATE/recorder.pid" ] && kill -0 "$(cat "$STATE/recorder.pid")" 2>/dev/null; then
    echo "decks/recorder/assets/record-stop.png"
else
    echo "decks/recorder/assets/record.png"
fi
