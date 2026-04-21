#!/bin/bash
# Toggle a HA light entity and print new state
# Usage: ha-light-toggle.sh <entity_id>
ENTITY="$1"

curl -s -H "Authorization: Bearer $HA_TOKEN" -H "Content-Type: application/json" \
  -d "{\"entity_id\": \"$ENTITY\"}" \
  "$HA_URL/api/services/light/toggle" > /dev/null 2>&1
