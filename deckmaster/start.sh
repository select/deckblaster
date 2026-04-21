#!/bin/bash
# Stream Deck startup wrapper.
# Starts background helpers then runs deckmaster in the foreground.
# Systemd kills the whole cgroup on stop, so no manual cleanup needed.

cd "$(dirname "$0")"

# Load secrets and local config (HA_TOKEN, HA_URL, DECK_API, ...)
# shellcheck source=/dev/null
source "${HOME}/.config/streamdeck.env" || true
export HA_TOKEN HA_URL DECK_API

# Ensure bun and uv are on PATH
export PATH="${HOME}/.bun/bin:${HOME}/.local/bin:${PATH}"

# Virtual desktop icon poller (renders desk-N.png every 3s)
python3 decks/vdesktop/vdesktop-render.py poll &

# Door sensor poller (fires 20s HTTP API alert on key 7 when a door opens)
uv run decks/ha/ha.py poll-doors &

# Deckmaster — foreground, this is what systemd tracks
# Calendar alerts are handled inside next-event.py on each 30s widget poll
exec ./deckmaster -deck decks/main.deck -brightness 60 -watch -api :9990
