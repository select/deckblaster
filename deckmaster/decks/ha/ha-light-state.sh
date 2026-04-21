#!/bin/bash
# Get HA entity state, output "on" or "off"
# Usage: ha-light-state.sh <entity_id>
ENTITY="$1"

curl -s -H "Authorization: Bearer $HA_TOKEN" \
  "$HA_URL/api/states/$ENTITY" 2>/dev/null | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('state', '?'))
except:
    print('?')
"
