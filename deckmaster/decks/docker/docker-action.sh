#!/bin/bash
# docker-action.sh <command> [arg]
# Commands:
#   toggle <slot>     — start/stop container in slot
#   page next|prev    — advance/retreat page
bun decks/docker/docker-render.js "$1" "$2" 2>/dev/null
