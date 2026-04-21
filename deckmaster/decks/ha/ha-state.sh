#!/bin/bash
# Get light state for a room from HA
# Usage: ha-state.sh <friendly_name>
# Outputs: "on" or "off"


if [ -z "$HA_TOKEN" ]; then
  echo "?" >&2
  exit 0
fi

AREA="$1"
AUTH="Authorization: Bearer $HA_TOKEN"

STATE=$(curl -s -H "$AUTH" "$HA_URL/api/states" 2>/dev/null | python3 -c "
import sys, json
data = json.load(sys.stdin)
area = '$AREA'
for e in data:
    if e['entity_id'].startswith('light.'):
        attrs = e.get('attributes', {})
        friendly = attrs.get('friendly_name', '')
        if friendly.lower() == area.lower():
            print(e['state'])
            break
else:
    print('?')
" 2>/dev/null)

echo "${STATE:-?}"
