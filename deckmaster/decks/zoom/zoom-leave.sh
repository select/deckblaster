#!/bin/bash
# Leave the Zoom meeting and clear state.
DISPLAY=:1
export DISPLAY
SCRIPT_DIR="$(dirname "$0")"

"$SCRIPT_DIR/zoom-send-key.sh" alt+q || exit 0
rm -f /tmp/streamdeck-zoom-state
