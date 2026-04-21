#!/bin/bash
# Poll HA light states and push icon updates to deckmaster via HTTP API
# Run as a background daemon alongside deckmaster
# Usage: ha-poll.sh

DECK_API="http://localhost:9990"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ASSETS="$SCRIPT_DIR/assets"
POLL_INTERVAL=5

# Map: key_index|entity_id|icon_base|label|color
BUTTONS=(
  "0|switch.blaues_zimmer_schrank|desk|Buero|#3399ff"
  "1|light.klavier|sofa|Wohnz.|#ff9900"
  "2|light.schlafzimmer_decke|bed|Schlafz.|#cc66ff"
)

# Track previous states to avoid unnecessary updates
declare -A PREV_STATES

get_state() {
  curl -s -H "Authorization: Bearer $HA_TOKEN" \
    "$HA_URL/api/states/$1" 2>/dev/null | python3 -c "
import sys, json
try:
    print(json.load(sys.stdin).get('state', '?'))
except:
    print('?')
" 2>/dev/null
}

push_key() {
  local idx="$1" label="$2" color="$3" icon="$4" fontsize="$5"
  curl -s -X POST "$DECK_API/key/$idx" \
    -d "{\"label\":\"$label\",\"color\":\"$color\",\"icon\":\"$icon\",\"fontsize\":$fontsize}" \
    > /dev/null 2>&1
}

while true; do
  for entry in "${BUTTONS[@]}"; do
    IFS='|' read -r idx entity icon_base label color <<< "$entry"

    state=$(get_state "$entity")
    prev="${PREV_STATES[$idx]:-}"

    if [ "$state" != "$prev" ]; then
      if [ "$state" = "on" ]; then
        icon_file="$ASSETS/${icon_base}-on.png"
      else
        icon_file="$ASSETS/${icon_base}-off.png"
      fi
      push_key "$idx" "$label" "$color" "$icon_file" 8
      PREV_STATES[$idx]="$state"
    fi
  done

  sleep "$POLL_INTERVAL"
done
