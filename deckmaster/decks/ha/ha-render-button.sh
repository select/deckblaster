#!/bin/bash
# Generate a Stream Deck button image with icon + on/off indicator + label
# Usage: ha-render-button.sh <entity_id> <icon_base> <label> <color_hex>
# Outputs path to generated PNG

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$SCRIPT_DIR/assets"
CACHE="/tmp/streamdeck-ha"
mkdir -p "$CACHE"

ENTITY="$1"
ICON_BASE="$2"

STATE=$(curl -s -H "Authorization: Bearer $HA_TOKEN" \
  "$HA_URL/api/states/$ENTITY" 2>/dev/null | python3 -c "
import sys, json
try:
    print(json.load(sys.stdin).get('state', '?'))
except:
    print('?')
" 2>/dev/null)

if [ "$STATE" = "on" ]; then
  echo "$ASSETS/${ICON_BASE}-on.png"
else
  echo "$ASSETS/${ICON_BASE}-off.png"
fi
