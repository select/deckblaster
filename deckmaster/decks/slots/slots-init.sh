#!/bin/bash
# Initialize the slot machine display via HTTP API when entering the slots deck.
# Called as a background task after deck navigation.
exec /usr/bin/python3 decks/slots/slots-game.py init
