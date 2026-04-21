#!/bin/bash
# Returns path to pre-rendered port key image for slot N (0-11).
bun decks/ports/ports-render.js "$1" 2>/dev/null
