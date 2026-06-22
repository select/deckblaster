#!/bin/bash
# Stream Deck startup wrapper.
# Starts background helpers then runs deckmaster in the foreground.
# Systemd kills the whole cgroup on stop, so no manual cleanup needed.

cd "$(dirname "$0")"

# Load secrets and local config (HA_TOKEN, HA_URL, DECK_API, ...)
# shellcheck source=/dev/null
source "${HOME}/.config/streamdeck.env" || true
export HA_TOKEN HA_URL DECK_API

# Import JIRA_* vars from zsh environment (systemd doesn't source .zshrc)
if [ -z "$JIRA_URL" ]; then
    eval "$(zsh -c 'source ~/.zshenv 2>/dev/null; source ~/.zprofile 2>/dev/null; source ~/.zshrc 2>/dev/null; printenv' 2>/dev/null | grep '^JIRA_' | sed 's/^/export /' | sed "s/=/='/" | sed "s/$/'/")"
fi
export JIRA_URL JIRA_USERNAME JIRA_API_TOKEN

# Ensure bun and uv are on PATH
export PATH="${HOME}/.bun/bin:${HOME}/.local/bin:${PATH}"

# Wayland support: auto-detect XAUTHORITY for XWayland access
if [ -z "$XAUTHORITY" ]; then
    xauth_file=$(find /run/user/$(id -u) -name '.mutter-Xwaylandauth.*' 2>/dev/null | head -1)
    if [ -n "$xauth_file" ]; then
        export XAUTHORITY="$xauth_file"
    fi
fi

# Virtual desktop icon poller (renders desk-N.png every 3s)
python3 decks/vdesktop/vdesktop-render.py poll &

# Door sensor poller (fires 20s HTTP API alert on key 7 when a door opens)
uv run decks/ha/ha.py poll-doors &

# Pre-generate GitHub badge so key 6 isn't blank on startup
bun decks/github/github-prs.js badge &

# Pre-generate Jira badge so key 5 isn't blank on startup
bun decks/jira/jira-issues.ts badge &

# Calendar event fetcher (refreshes 7 days of events every 5 min)
python3 decks/calendar/calendar-fetch.py poll &

# Deckmaster — foreground, this is what systemd tracks
# Calendar alerts are handled inside next-event.js on each 5s widget poll
exec ./deckmaster -deck decks/main.deck -brightness 38 -watch -api :9990
