#!/bin/bash
# Output light state as visual indicator
# Usage: ha-indicator.sh <entity_id> <label>
# Outputs two ;-separated values for command widget: "label;● ON" or "label;○ OFF"
ENTITY="$1"
LABEL="$2"

STATE=$(curl -s -H "Authorization: Bearer $HA_TOKEN" \
  "$HA_URL/api/states/$ENTITY" 2>/dev/null | python3 -c "
import sys, json
try:
    print(json.load(sys.stdin).get('state', '?'))
except:
    print('?')
" 2>/dev/null)

if [ "$STATE" = "on" ]; then
  echo "ON"
else
  echo "OFF"
fi
