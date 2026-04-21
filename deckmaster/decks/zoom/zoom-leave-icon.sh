#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$SCRIPT_DIR/assets"
DISPLAY=:1; export DISPLAY

if ! xdotool search --limit 1 --class zoom &>/dev/null; then
    echo "$ASSETS/no-meeting-leave.png"; exit 0
fi

echo "$ASSETS/leave.png"
