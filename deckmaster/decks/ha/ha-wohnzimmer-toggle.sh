#!/bin/bash
# Toggle all lights in a Wohnzimmer (multiple entities)
AUTH="Authorization: Bearer $HA_TOKEN"

# Check if Klavier (main light) is on
STATE=$(curl -s -H "$AUTH" "$HA_URL/api/states/light.klavier" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('state','off'))" 2>/dev/null)

if [ "$STATE" = "on" ]; then
  SVC="turn_off"
else
  SVC="turn_on"
fi

for entity in light.klavier light.tv_lampe light.esstisch light.fenster_wohnzimmer; do
  curl -s -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"entity_id\": \"$entity\"}" \
    "$HA_URL/api/services/light/$SVC" > /dev/null 2>&1
done
