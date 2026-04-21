#!/usr/bin/env python3
"""
Render category button icon for polymarket category selection page.
Dynamically fetches categories from state/cache.

Usage: polymarket-cat-icon.py <slot>  (slot 0-8)
"""

import json
import os
import sys
from PIL import Image, ImageDraw, ImageFont

DATA_DIR = os.path.expanduser("~/.local/share/streamdeck-polymarket")
STATE_FILE = os.path.join(DATA_DIR, "state.json")
CATS_CACHE = os.path.join(DATA_DIR, "categories.json")
TMP = "/tmp/streamdeck-polymarket"
EMPTY = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets", "empty.png")
os.makedirs(TMP, exist_ok=True)

SIZE = 72
CATS_PER_PAGE = 9

try:
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    FONT_XS = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 9)
except Exception:
    FONT_LG = FONT_XS = ImageFont.load_default()


slot = int(sys.argv[1]) if len(sys.argv) > 1 else 0

if not os.path.exists(STATE_FILE) or not os.path.exists(CATS_CACHE):
    print(EMPTY)
    sys.exit(0)

try:
    with open(STATE_FILE) as f:
        state = json.load(f)
    with open(CATS_CACHE) as f:
        cats = json.load(f)
except Exception:
    print(EMPTY)
    sys.exit(0)

cat_page = state.get("cat_page", 0)
selected_name = state.get("cat_name", "Popular")

# Paginate
total_pages = max(1, (len(cats) + CATS_PER_PAGE - 1) // CATS_PER_PAGE)
cat_page = cat_page % total_pages
start = cat_page * CATS_PER_PAGE
page_cats = cats[start:start + CATS_PER_PAGE]

if slot >= len(page_cats):
    print(EMPTY)
    sys.exit(0)

cat = page_cats[slot]
name = cat["name"]
is_selected = (name == selected_name)
visit_count = cat.get("visits", 0)

img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)

if is_selected:
    for y in range(SIZE):
        t = y / SIZE
        draw.line([(0, y), (SIZE - 1, y)],
                  fill=(int(20 + 25 * t), int(30 + 20 * t), int(60 + 20 * t)))
    draw.rounded_rectangle([2, 2, 70, 70], radius=8, outline=(80, 160, 255), width=3)
else:
    for y in range(SIZE):
        t = y / SIZE
        draw.line([(0, y), (SIZE - 1, y)],
                  fill=(int(18 - 8 * t), int(18 - 8 * t), int(28 - 12 * t)))
    draw.rounded_rectangle([2, 2, 70, 70], radius=8, outline=(50, 50, 70), width=1)

# Category name centered
bbox = draw.textbbox((0, 0), name, font=FONT_LG)
tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
name_color = (120, 200, 255) if is_selected else (200, 200, 220)
draw.text(((SIZE - tw) // 2, (SIZE - th) // 2 - 4), name, fill=name_color, font=FONT_LG)

# Visit count
if visit_count > 0:
    vtext = f"x{visit_count}"
    bbox3 = draw.textbbox((0, 0), vtext, font=FONT_XS)
    tw3 = bbox3[2] - bbox3[0]
    draw.text(((SIZE - tw3) // 2, 56), vtext, fill=(100, 100, 130), font=FONT_XS)

# Selected dot
if is_selected:
    draw.ellipse([56, 6, 66, 16], fill=(80, 200, 100))

path = os.path.join(TMP, f"cat-{slot}.png")
img.save(path)
print(path)
