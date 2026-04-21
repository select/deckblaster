#!/bin/bash
# Toggle deck recording on/off. Updates key icon via API.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="/tmp/streamdeck-recording/recorder.pid"
REC_ICON="$SCRIPT_DIR/assets/record.png"
STOP_ICON="$SCRIPT_DIR/assets/record-stop.png"
KEY=3  # key index on main deck

if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    # Currently recording → stop
    python3 "$SCRIPT_DIR/deck-record.py" stop
else
    # Not recording → start
    python3 "$SCRIPT_DIR/deck-record.py" start --fps 5
fi
