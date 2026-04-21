#!/bin/bash
# Output two lines for a plant moisture command widget:
#   line 1 – plant label
#   line 2 – moisture percentage, e.g. "42%"
# Usage: ha-moisture-text.sh <entity_id> <label>

ENTITY="$1"
LABEL="$2"

python3 - "$ENTITY" "$LABEL" "$HA_URL" "$HA_TOKEN" <<'PYEOF'
import sys, json, urllib.request

entity, label, url, token = sys.argv[1:]

try:
    req = urllib.request.Request(
        f"{url}/api/states/{entity}",
        headers={"Authorization": f"Bearer {token}"}
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        data = json.loads(resp.read())
    raw = data.get("state", "?")
    try:
        val = f"{float(raw):.0f}%"
    except Exception:
        val = raw
except Exception:
    val = "?"

print(label)
print(val)
PYEOF
