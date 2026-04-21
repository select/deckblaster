#!/bin/bash
# Output two lines for a door command widget:
#   line 1 – door label
#   line 2 – "OPEN!" when open, or "Xm ago / Xh ago / Xd ago" when closed
# Usage: ha-door-text.sh <entity_id> <label>

ENTITY="$1"
LABEL="$2"

python3 - "$ENTITY" "$LABEL" "$HA_URL" "$HA_TOKEN" <<'PYEOF'
import sys, json, urllib.request
from datetime import datetime, timezone

entity, label, url, token = sys.argv[1:]

try:
    req = urllib.request.Request(
        f"{url}/api/states/{entity}",
        headers={"Authorization": f"Bearer {token}"}
    )
    with urllib.request.urlopen(req, timeout=5) as resp:
        data = json.loads(resp.read())
    state        = data.get("state", "?")
    last_changed = data.get("last_changed", "")
except Exception:
    print(label)
    print("?")
    sys.exit(0)

print(label)

if state == "on":
    print("OPEN!")
else:
    try:
        dt   = datetime.fromisoformat(last_changed.replace("Z", "+00:00"))
        secs = (datetime.now(timezone.utc) - dt).total_seconds()
        if secs < 60:
            print("just now")
        elif secs < 3600:
            print(f"{int(secs // 60)}m ago")
        elif secs < 86400:
            print(f"{int(secs // 3600)}h ago")
        else:
            d = int(secs // 86400)
            print(f"{d}d ago")
    except Exception:
        print("?")
PYEOF
