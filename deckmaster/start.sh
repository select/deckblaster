#!/bin/bash
# Stream Deck startup wrapper.
# Starts background helpers then runs deckmaster in the foreground.
# Systemd kills the whole cgroup on stop, so no manual cleanup needed.

cd "$(dirname "$0")"

# Load secrets and local config (HA_TOKEN, HA_URL, DECK_API, ...)
# shellcheck source=/dev/null
source "${HOME}/.config/streamdeck.env" || true
export HA_TOKEN HA_URL DECK_API

# Ensure bun is on PATH (installed to ~/.bun/bin by default)
export PATH="${HOME}/.bun/bin:${PATH}"

# Virtual desktop icon poller (renders desk-N.png every 3s)
decks/vdesktop/vdesktop-poll.sh &

# Door sensor poller (fires 20s HTTP API alert on key 7 when a door opens)
decks/ha/ha-door-poll.sh &

# Deckmaster — foreground, this is what systemd tracks
# Calendar alerts are handled inside next-event.py on each 30s widget poll
exec ./deckmaster -deck decks/main.deck -brightness 60 -watch -api :9990
