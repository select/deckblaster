#!/bin/bash
# Poll HA door sensors and push a 20s alert to the Stream Deck when any door opens.
# The alert appears on key 7 (the HA-page button on the main main.deck).
# Run as background daemon launched by start.sh.

DECK_API="http://localhost:9990"
POLL_INTERVAL=5

# Format: key_index|entity_id|short_label
# key indices are the physical Stream Deck key numbers
DOORS=(
  "3|binary_sensor.haustur_contact|HAUSTÜR"
  "4|binary_sensor.terrassentur_contact|TERRASSE"
  "5|binary_sensor.kuchentur_contact|KÜCHENTÜR"
  "6|binary_sensor.fahrrad_box_contact|FAHRRAD"
)

# Key 7 on the main deck is the HA-page shortcut — we flash it on door open
HA_BUTTON_KEY=7

declare -A PREV_STATES

get_state() {
  curl -s -H "Authorization: Bearer $HA_TOKEN" \
    "$HA_URL/api/states/$1" 2>/dev/null | python3 -c "
import sys, json
try: print(json.load(sys.stdin).get('state', '?'))
except: print('?')
" 2>/dev/null
}

push_door_alert() {
  local label="$1"
  local payload
  payload=$(python3 -c "
import json
print(json.dumps({
  'label':      '$label',
  'color':      '#ffffff',
  'background': '#cc2200',
  'fontsize':   12,
  'duration':   '20s'
}))
")
  curl -s -X POST "$DECK_API/key/$HA_BUTTON_KEY" \
    -H "Content-Type: application/json" \
    -d "$payload" > /dev/null 2>&1
}

while true; do
  for entry in "${DOORS[@]}"; do
    IFS='|' read -r idx entity label <<< "$entry"

    state=$(get_state "$entity")
    prev="${PREV_STATES[$idx]:-}"   # empty on first iteration → no alert

    # Only alert on off→on transition (door just opened)
    if [ -n "$prev" ] && [ "$state" = "on" ] && [ "$prev" = "off" ]; then
      push_door_alert "🚪 ${label}"
    fi

    PREV_STATES[$idx]="$state"
  done

  sleep "$POLL_INTERVAL"
done
