#!/bin/bash
# Returns path to pre-rendered docker slot image for slot N.
bun decks/docker/docker-render.js slot "$1" 2>/dev/null
