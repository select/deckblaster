#!/usr/bin/env python3
"""
Shared utility: load pywal colors from ~/.cache/wal/colors.json.
Colors 1-7 (skipping background color0).
"""

import json
import os

WAL_COLORS_FILE = os.path.expanduser("~/.cache/wal/colors.json")

_FALLBACK = [
    ("#95281D", "DkRed"),
    ("#DB601B", "Orange"),
    ("#AF414D", "Rose"),
    ("#F39E21", "Amber"),
    ("#18608F", "Blue"),
    ("#DC588C", "Pink"),
    ("#c2c1c5", "Grey"),
]


def load_wal_colors():
    """Load colors 1-7 from pywal, fallback to hardcoded defaults."""
    try:
        with open(WAL_COLORS_FILE) as f:
            data = json.load(f)
        colors = []
        names = ["DkRed", "Orange", "Rose", "Amber", "Blue", "Pink", "Grey"]
        for i in range(1, 8):
            hex_val = data["colors"][f"color{i}"]
            colors.append((hex_val, names[i - 1]))
        return colors
    except Exception:
        return list(_FALLBACK)


def hex_to_rgb(h):
    return (int(h[1:3], 16), int(h[3:5], 16), int(h[5:7], 16))
