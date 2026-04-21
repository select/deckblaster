#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$SCRIPT_DIR/assets"
STATE_FILE="/tmp/streamdeck-zoom-state"
DISPLAY=:1; export DISPLAY

if ! xdotool search --limit 1 --class zoom &>/dev/null; then
    echo "$ASSETS/no-meeting-video.png"; exit 0
fi

source "$STATE_FILE" 2>/dev/null
[[ "${video_off:-0}" == "1" ]] && echo "$ASSETS/video-off.png" || echo "$ASSETS/video-on.png"
