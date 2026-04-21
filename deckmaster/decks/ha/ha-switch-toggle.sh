#!/bin/bash
# Toggle a HA switch entity
# Usage: ha-switch-toggle.sh <entity_id>
ENTITY="$1"

curl -s -H "Authorization: Bearer $HA_TOKEN" -H "Content-Type: application/json" \
  -d "{\"entity_id\": \"$ENTITY\"}" \
  "$HA_URL/api/services/switch/toggle" > /dev/null 2>&1
