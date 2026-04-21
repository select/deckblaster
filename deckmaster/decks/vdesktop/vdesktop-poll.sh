#!/bin/bash
# Poller daemon: re-renders all virtual desktop images every 3 seconds.
# Run once at startup; all vdesktop-icon.sh calls just read the pre-rendered PNGs.

export DISPLAY=:1

while true; do
  python3 decks/vdesktop/vdesktop-render.py > /dev/null 2>&1
  sleep 3
done
