#!/bin/bash
# Copy the correct on/off icon variant to a stable path for deckmaster
# Usage: ha-icon-update.sh <entity_id> <icon_base>
# Creates/updates /tmp/streamdeck-ha/<icon_base>.png

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$SCRIPT_DIR/assets"
CACHE="/tmp/streamdeck-ha"
mkdir -p "$CACHE"

ENTITY="$1"
ICON_BASE="$2"
OUT="$CACHE/${ICON_BASE}.png"

STATE=$(curl -s -H "Authorization: Bearer $HA_TOKEN" \
  "$HA_URL/api/states/$ENTITY" 2>/dev/null | python3 -c "
import sys, json
try:
    print(json.load(sys.stdin).get('state', '?'))
except:
    print('?')
" 2>/dev/null)

if [ "$STATE" = "on" ]; then
  cp "$ASSETS/${ICON_BASE}-on.png" "$OUT"
else
  cp "$ASSETS/${ICON_BASE}-off.png" "$OUT"
fi
