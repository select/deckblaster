#!/bin/bash
# Open the URL found in the location field of the next/current calendar event.
# Zoom links and regular https:// URLs are both handled by xdg-open.
CACHE="/tmp/streamdeck-next-event.json"

if [ ! -f "$CACHE" ]; then
    exit 0
fi

location=$(python3 -c "
import json, sys
with open('$CACHE') as f:
    d = json.load(f)
print(d.get('location', ''))
" 2>/dev/null)

# Extract the first URL from the location string (could be plain text with a URL embedded)
url=$(echo "$location" | grep -oP 'https?://\S+' | head -1)

if [ -n "$url" ]; then
    DISPLAY=:1 xdg-open "$url" &
fi
