#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Render ALL 9 category slot icons in one process (fast).
Called by cat-init, cat-prev, cat-next.
Outputs nothing — writes to /tmp/streamdeck-polymarket/cat-*.png
"""

import json
import os
import sys
from PIL import Image, ImageDraw, ImageFont

DATA_DIR = os.path.expanduser("~/.local/share/streamdeck-polymarket")
STATE_FILE = os.path.join(DATA_DIR, "state.json")
CATS_CACHE = os.path.join(DATA_DIR, "categories.json")
TMP = "/tmp/streamdeck-polymarket"
EMPTY_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets", "empty.png")
os.makedirs(TMP, exist_ok=True)

SIZE = 72
CATS_PER_PAGE = 9

try:
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 12)
    FONT_XS = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 9)
except Exception:
    FONT_LG = FONT_MD = FONT_XS = ImageFont.load_default()

# Load empty image once
EMPTY_IMG = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))


def render_cat(name, is_selected, visit_count, bet_count=0):
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

    color = (120, 200, 255) if is_selected else (200, 200, 220)
    max_w = SIZE - 12  # 60px usable

    # Try large font first, fall back to medium if any line overflows
    for font, line_h in [(FONT_LG, 16), (FONT_MD, 14)]:
        words = name.split()
        lines = []
        current = ""
        for w in words:
            test = f"{current} {w}".strip()
            bbox = draw.textbbox((0, 0), test, font=font)
            if bbox[2] - bbox[0] > max_w and current:
                lines.append(current)
                current = w
            else:
                current = test
        if current:
            lines.append(current)

        # Check all lines fit
        fits = all(draw.textbbox((0, 0), l, font=font)[2] - draw.textbbox((0, 0), l, font=font)[0] <= max_w for l in lines)
        if fits:
            break

    total_h = len(lines) * line_h
    y_start = (SIZE - total_h) // 2 - 2
    if bet_count > 0:
        y_start -= 6  # make room for bet count

    for i, line in enumerate(lines):
        bbox = draw.textbbox((0, 0), line, font=font)
        tw = bbox[2] - bbox[0]
        draw.text(((SIZE - tw) // 2, y_start + i * line_h), line, fill=color, font=font)

    if bet_count > 0:
        vtext = f"{bet_count} bets"
        bbox3 = draw.textbbox((0, 0), vtext, font=FONT_XS)
        tw3 = bbox3[2] - bbox3[0]
        draw.text(((SIZE - tw3) // 2, 56), vtext, fill=(100, 100, 130), font=FONT_XS)

    if is_selected:
        draw.ellipse([6, 6, 16, 16], fill=(80, 200, 100))

    if visit_count > 0:
        vtext = f"x{visit_count}"
        bbox4 = draw.textbbox((0, 0), vtext, font=FONT_XS)
        tw4 = bbox4[2] - bbox4[0]
        draw.text((SIZE - tw4 - 6, 4), vtext, fill=(140, 140, 100), font=FONT_XS)

    return img


def render_all():
    if not os.path.exists(STATE_FILE) or not os.path.exists(CATS_CACHE):
        for i in range(CATS_PER_PAGE):
            EMPTY_IMG.save(os.path.join(TMP, f"cat-{i}.png"))
        EMPTY_IMG.save(os.path.join(TMP, "cat-page.png"))
        return

    with open(STATE_FILE) as f:
        state = json.load(f)
    with open(CATS_CACHE) as f:
        cats = json.load(f)

    cat_page = state.get("cat_page", 0)
    selected_name = state.get("cat_name", "Popular")

    total_pages = max(1, (len(cats) + CATS_PER_PAGE - 1) // CATS_PER_PAGE)
    cat_page = min(cat_page, total_pages - 1)
    start = cat_page * CATS_PER_PAGE
    page_cats = cats[start:start + CATS_PER_PAGE]

    for i in range(CATS_PER_PAGE):
        path = os.path.join(TMP, f"cat-{i}.png")
        if i < len(page_cats):
            cat = page_cats[i]
            img = render_cat(cat["name"], cat["name"] == selected_name, cat.get("visits", 0), cat.get("count", 0))
            img.save(path)
        else:
            EMPTY_IMG.save(path)

    # Page indicator
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    page_text = f"{cat_page + 1}/{total_pages}"
    bbox = draw.textbbox((0, 0), page_text, font=FONT_LG)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
    draw.text(((SIZE - tw) // 2, (SIZE - th) // 2), page_text, fill=(100, 100, 140), font=FONT_LG)
    img.save(os.path.join(TMP, "cat-page.png"))


render_all()
