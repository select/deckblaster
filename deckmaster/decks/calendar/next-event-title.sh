#!/bin/bash
python3 decks/calendar/next-event.py title 2>/dev/null || echo "-"
