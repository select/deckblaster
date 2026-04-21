#!/bin/bash
# Toggle a light in Home Assistant
# Usage: ha-toggle.sh <entity_domain> <area> [name]
# Checks current state, then flips it.

# Token from environment or fallback

if [ -z "$HA_TOKEN" ]; then
  echo "No HA_TOKEN" >&2
  exit 1
fi

AREA="$1"
shift

AUTH="Authorization: Bearer $HA_TOKEN"

# Find light entities in this area, get their state
STATES=$(curl -s -H "$AUTH" "$HA_URL/api/states" 2>/dev/null)

# Find the main light group for the area (domain=light, area matches)
# We'll toggle all lights in the area by using the area target
ENTITY=$(echo "$STATES" | python3 -c "
import sys, json
data = json.load(sys.stdin)
area = '$AREA'
# Find light entities with matching area in attributes
for e in data:
    if e['entity_id'].startswith('light.'):
        attrs = e.get('attributes', {})
        friendly = attrs.get('friendly_name', '')
        if friendly.lower() == area.lower():
            print(e['entity_id'] + '|' + e['state'])
            break
" 2>/dev/null)

if [ -z "$ENTITY" ]; then
  echo "off"
  exit 0
fi

ENTITY_ID=$(echo "$ENTITY" | cut -d'|' -f1)
STATE=$(echo "$ENTITY" | cut -d'|' -f2)

if [ "$STATE" = "on" ]; then
  # Turn off
  curl -s -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"entity_id\": \"$ENTITY_ID\"}" \
    "$HA_URL/api/services/light/turn_off" > /dev/null 2>&1
  echo "off"
else
  # Turn on
  curl -s -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"entity_id\": \"$ENTITY_ID\"}" \
    "$HA_URL/api/services/light/turn_on" > /dev/null 2>&1
  echo "on"
fi
