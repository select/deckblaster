#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Render dynamic button icons for the mouse highlighter deck.
Reads state + pywal colors fresh on every call.

Usage: highlight-btn-icon.py <type>
Types: size-up, size-down, color, alpha-up, alpha-down
"""

import json
import os
import sys
from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from wal_colors import load_wal_colors, hex_to_rgb

SIZE = 72
TMP = "/tmp/streamdeck-highlight"
STATE_FILE = os.path.expanduser("~/.local/share/streamdeck-highlight.json")
os.makedirs(TMP, exist_ok=True)

RADII = [10, 15, 20, 30, 40, 50, 70]
OPACITIES = [0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]

try:
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 10)
except Exception:
    FONT_SM = ImageFont.load_default()


def load_state():
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            return json.load(f)
    return {"active": False, "radius_idx": 2, "color_idx": 0, "opacity_idx": 3}


def gradient(draw, y0, y1, c_top, c_bot):
    for y in range(y0, y1):
        t = (y - y0) / max(y1 - y0, 1)
        r = int(c_top[0] + (c_bot[0] - c_top[0]) * t)
        g = int(c_top[1] + (c_bot[1] - c_top[1]) * t)
        b = int(c_top[2] + (c_bot[2] - c_top[2]) * t)
        draw.line([(0, y), (SIZE - 1, y)], fill=(r, g, b))


def render_size_up(state, colors):
    """Bigger — just a large dot, no arrows."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, 0, SIZE, (25, 40, 55), (12, 20, 30))
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(50, 100, 160), width=2)

    ridx = state.get("radius_idx", 2)
    at_max = ridx >= len(RADII) - 1
    color = hex_to_rgb(colors[state.get("color_idx", 0) % len(colors)][0])
    opacity = OPACITIES[min(state.get("opacity_idx", 3), len(OPACITIES) - 1)]
    alpha = int(opacity * 255)

    # Large dot to suggest "bigger"
    dot_r = min(28, max(16, RADII[min(ridx, len(RADII) - 1)] // 2 + 8))
    cx, cy = 36, 36
    if at_max:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha // 2), outline=(60, 60, 80), width=1)
    else:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha))

    path = os.path.join(TMP, "btn-size-up.png")
    img.save(path)
    return path


def render_size_down(state, colors):
    """Smaller — just a small dot, no arrows."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, 0, SIZE, (25, 40, 55), (12, 20, 30))
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(50, 100, 160), width=2)

    ridx = state.get("radius_idx", 2)
    at_min = ridx <= 0
    color = hex_to_rgb(colors[state.get("color_idx", 0) % len(colors)][0])
    opacity = OPACITIES[min(state.get("opacity_idx", 3), len(OPACITIES) - 1)]
    alpha = int(opacity * 255)

    # Small dot to suggest "smaller"
    dot_r = max(4, RADII[min(ridx, len(RADII) - 1)] // 4)
    cx, cy = 36, 36
    if at_min:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha // 2), outline=(60, 60, 80), width=1)
    else:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha))

    path = os.path.join(TMP, "btn-size-down.png")
    img.save(path)
    return path


def render_color_next(state, colors):
    """Top color button — shows the NEXT color as a circle. Press to select it."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)

    cidx = state.get("color_idx", 0) % len(colors)
    next_idx = (cidx + 1) % len(colors)
    next_rgb = hex_to_rgb(colors[next_idx][0])

    cx, cy = 36, 36
    r = 24
    draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=next_rgb)

    path = os.path.join(TMP, "btn-color-next.png")
    img.save(path)
    return path


def render_color_prev(state, colors):
    """Bottom color button — shows the PREVIOUS color as a circle. Press to select it."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)

    cidx = state.get("color_idx", 0) % len(colors)
    prev_idx = (cidx - 1) % len(colors)
    prev_rgb = hex_to_rgb(colors[prev_idx][0])

    cx, cy = 36, 36
    r = 24
    draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=prev_rgb)

    path = os.path.join(TMP, "btn-color-prev.png")
    img.save(path)
    return path


def render_alpha_up(state, colors):
    """More opaque — brighter dot + percentage."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, 0, SIZE, (50, 45, 15), (28, 25, 8))
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(180, 140, 30), width=2)

    oidx = state.get("opacity_idx", 3)
    at_max = oidx >= len(OPACITIES) - 1
    color = hex_to_rgb(colors[state.get("color_idx", 0) % len(colors)][0])

    next_idx = min(oidx + 1, len(OPACITIES) - 1)
    next_op = OPACITIES[next_idx]
    alpha = int(next_op * 255)
    cx, cy = 36, 32
    dot_r = 16

    if at_max:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha // 2), outline=(60, 55, 30), width=1)
    else:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha))

    pct = f"{int(next_op * 100)}%"
    bbox = draw.textbbox((0, 0), pct, font=FONT_SM)
    tw = bbox[2] - bbox[0]
    lbl_color = (60, 55, 30) if at_max else (255, 230, 120)
    draw.text(((SIZE - tw) // 2, 54), pct, fill=lbl_color, font=FONT_SM)

    path = os.path.join(TMP, "btn-alpha-up.png")
    img.save(path)
    return path


def render_alpha_down(state, colors):
    """Less opaque — fainter dot + percentage."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, 0, SIZE, (50, 45, 15), (28, 25, 8))
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(180, 140, 30), width=2)

    oidx = state.get("opacity_idx", 3)
    at_min = oidx <= 0
    color = hex_to_rgb(colors[state.get("color_idx", 0) % len(colors)][0])

    prev_idx = max(oidx - 1, 0)
    prev_op = OPACITIES[prev_idx]
    alpha = int(prev_op * 255)
    cx, cy = 36, 32
    dot_r = 16

    if at_min:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha // 2), outline=(60, 55, 30), width=1)
    else:
        draw.ellipse([cx - dot_r, cy - dot_r, cx + dot_r, cy + dot_r],
                     fill=(*color, alpha))

    pct = f"{int(prev_op * 100)}%"
    bbox = draw.textbbox((0, 0), pct, font=FONT_SM)
    tw = bbox[2] - bbox[0]
    lbl_color = (60, 55, 30) if at_min else (255, 230, 120)
    draw.text(((SIZE - tw) // 2, 54), pct, fill=lbl_color, font=FONT_SM)

    path = os.path.join(TMP, "btn-alpha-down.png")
    img.save(path)
    return path


RENDERERS = {
    "size-up": render_size_up,
    "size-down": render_size_down,
    "color-next": render_color_next,
    "color-prev": render_color_prev,
    "alpha-up": render_alpha_up,
    "alpha-down": render_alpha_down,
}

if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] not in RENDERERS:
        print(f"Usage: {sys.argv[0]} {{{','.join(RENDERERS.keys())}}}", file=sys.stderr)
        sys.exit(1)

    state = load_state()
    colors = load_wal_colors()
    path = RENDERERS[sys.argv[1]](state, colors)
    print(path)
