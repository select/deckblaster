#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""Render a single text key for polymarket. Called by icon_command.
Uses monospace font on a fixed 10x5 character grid for predictable wrapping."""

import json
import os
import sys
import math
from PIL import Image, ImageDraw, ImageFont

DATA_DIR = os.path.expanduser("~/.local/share/streamdeck-polymarket")
STATE_FILE = os.path.join(DATA_DIR, "state.json")
CACHE_FILE_BASE = "/tmp/streamdeck-polymarket-cache"
TMP = "/tmp/streamdeck-polymarket"
EMPTY = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets", "empty.png")
os.makedirs(TMP, exist_ok=True)

SIZE = 72
BETS_PER_PAGE = 3

# Monospace grid: 9 cols x 4 rows
COLS = 9
ROWS = 4
SCROLL_STEP = 1  # scroll by 1 line per click

try:
    FONT = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf", 12)
    FONT_XS = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 9)
except Exception:
    FONT = FONT_XS = ImageFont.load_default()

CHAR_W = 7
CHAR_H = 13  # line height with spacing


def format_volume(v):
    if v >= 1_000_000:
        return f"${v / 1_000_000:.1f}M"
    elif v >= 1_000:
        return f"${v / 1_000:.0f}K"
    return f"${v:.0f}"


def wrap_text(text, width):
    """Word-wrap text to fixed width. Never breaks mid-word."""
    words = text.split()
    lines = []
    current = ""
    for w in words:
        if not current:
            current = w
        elif len(current) + 1 + len(w) <= width:
            current += " " + w
        else:
            lines.append(current)
            current = w
    if current:
        lines.append(current)
    return lines


slot = int(sys.argv[1]) if len(sys.argv) > 1 else 0

if not os.path.exists(STATE_FILE):
    print(EMPTY)
    sys.exit(0)

try:
    with open(STATE_FILE) as f:
        state = json.load(f)
except (json.JSONDecodeError, IOError):
    # Retry once — atomic write might be in progress
    import time
    time.sleep(0.05)
    try:
        with open(STATE_FILE) as f:
            state = json.load(f)
    except Exception:
        print(EMPTY)
        sys.exit(0)

if state.get("loading"):
    print(EMPTY)
    sys.exit(0)

cat_tag_id = state.get("cat_tag_id", None)
cat_query = state.get("cat_query", None)
cache_key = cat_tag_id or cat_query or "popular"
cache_path = f"{CACHE_FILE_BASE}-{str(cache_key).replace(' ','_')}.json"

if not os.path.exists(cache_path):
    print(EMPTY)
    sys.exit(0)

try:
    with open(cache_path) as f:
        cache = json.load(f)
except (json.JSONDecodeError, IOError):
    print(EMPTY)
    sys.exit(0)

markets = cache.get("markets", [])
page = state.get("page", 0)
start = page * BETS_PER_PAGE
idx = start + slot

if idx >= len(markets):
    print(EMPTY)
    sys.exit(0)

market = markets[idx]
frames = state.get("frames", [0, 0, 0])
frame = frames[slot] if slot < len(frames) else 0

question = market["question"]
volume = market["volume"]

# Wrap to grid width
all_lines = wrap_text(question, COLS)
max_scroll = max(0, len(all_lines) - ROWS)
total_steps = max(1, math.ceil(max_scroll / SCROLL_STEP) + 1) if max_scroll > 0 else 1

# Clamp frame — never wrap, just stay at last page
current_step = min(frame, total_steps - 1)
scroll_pos = min(current_step * SCROLL_STEP, max_scroll)
visible = all_lines[scroll_pos:scroll_pos + ROWS]

# Render
img = Image.new("RGBA", (SIZE, SIZE), (12, 12, 20, 255))
draw = ImageDraw.Draw(img)
draw.rounded_rectangle([1, 1, 71, 71], radius=6, outline=(50, 50, 70), width=1)

# Volume at top
vol_text = format_volume(volume)
bbox = draw.textbbox((0, 0), vol_text, font=FONT_XS)
tw = bbox[2] - bbox[0]
draw.text(((SIZE - tw) // 2, 2), vol_text, fill=(120, 120, 160), font=FONT_XS)

# Text lines
y = 14
x_offset = (SIZE - COLS * CHAR_W) // 2  # center the grid
for line in visible:
    draw.text((x_offset, y), line, fill=(220, 220, 240), font=FONT)
    y += CHAR_H

# Page dots
if total_steps > 1:
    dot_y = SIZE - 11
    dot_r = 3  # radius
    dot_spacing = 10
    total_dot_w = (total_steps - 1) * dot_spacing
    dot_cx_start = (SIZE - total_dot_w) // 2
    for i in range(total_steps):
        cx = dot_cx_start + i * dot_spacing
        if i == current_step:
            draw.ellipse([cx - dot_r, dot_y - dot_r, cx + dot_r, dot_y + dot_r],
                         fill=(100, 160, 255))
        else:
            draw.ellipse([cx - dot_r, dot_y - dot_r, cx + dot_r, dot_y + dot_r],
                         outline=(150, 180, 220), width=1)

path = os.path.join(TMP, f"text-{slot}.png")
img.save(path)
print(path)
