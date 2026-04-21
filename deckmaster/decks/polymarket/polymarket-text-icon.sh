#!/bin/bash
# Render text icon for polymarket bet slot.
# Called by icon_command on keys 6, 7, 8.
# Reads state to determine page and frame, renders and prints path.
SLOT="$1"
STATE="/tmp/streamdeck-polymarket.json"
EMPTY="decks/assets/empty.png"

if [ ! -f "$STATE" ]; then
    echo "$EMPTY"
    exit 0
fi

python3 decks/polymarket/polymarket-render-text.py "$SLOT"
