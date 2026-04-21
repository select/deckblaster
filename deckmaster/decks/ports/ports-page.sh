#!/bin/bash
# Advance or retreat the ports page: ports-page.sh next|prev
bun decks/ports/ports-render.js page "$1" 2>/dev/null
