#!/usr/bin/env python3
"""Generate mouse highlighter button images for Stream Deck."""

import os
from PIL import Image, ImageDraw, ImageFont

SIZE = 72
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
os.makedirs(OUT, exist_ok=True)

try:
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 20)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
    FONT_XS = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 9)
except Exception:
    FONT_LG = FONT_MD = FONT_SM = FONT_XS = ImageFont.load_default()


def gradient(draw, size, c_top, c_bot):
    for y in range(size):
        t = y / size
        r = int(c_top[0] + (c_bot[0] - c_top[0]) * t)
        g = int(c_top[1] + (c_bot[1] - c_top[1]) * t)
        b = int(c_top[2] + (c_bot[2] - c_top[2]) * t)
        draw.line([(0, y), (size - 1, y)], fill=(r, g, b))


def make_button(label, sublabel, bg_top, bg_bot, outline, text_color, icon_fn=None):
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, SIZE, bg_top, bg_bot)
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=outline, width=2)

    if icon_fn:
        icon_fn(draw)

    if sublabel:
        bbox = draw.textbbox((0, 0), label, font=FONT_MD)
        tw = bbox[2] - bbox[0]
        draw.text(((SIZE - tw) // 2, 16), label, fill=text_color, font=FONT_MD)

        bbox2 = draw.textbbox((0, 0), sublabel, font=FONT_XS)
        tw2 = bbox2[2] - bbox2[0]
        draw.text(((SIZE - tw2) // 2, 48), sublabel, fill=(160, 160, 160), font=FONT_XS)
    else:
        bbox = draw.textbbox((0, 0), label, font=FONT_LG)
        tw = bbox[2] - bbox[0]
        th = bbox[3] - bbox[1]
        draw.text(((SIZE - tw) // 2, (SIZE - th) // 2), label, fill=text_color, font=FONT_LG)

    return img


# ── ON/OFF toggle ──────────────────────────────────────────────────────────────
def draw_power_icon(draw):
    # Power symbol
    cx, cy = 36, 32
    draw.arc([cx - 14, cy - 14, cx + 14, cy + 14], start=-60, end=240, fill=(200, 200, 200), width=3)
    draw.line([cx, cy - 16, cx, cy - 4], fill=(200, 200, 200), width=3)

img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (30, 60, 30), (15, 35, 15))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(0, 200, 0), width=2)
draw_power_icon(draw)
draw.text((12, 52), "ON/OFF", fill=(0, 220, 80), font=FONT_SM)
img.save(os.path.join(OUT, "toggle.png"))
print("  ✓ toggle.png")

# ── Size Up ────────────────────────────────────────────────────────────────────
def draw_size_up(draw):
    cx, cy = 36, 28
    draw.ellipse([cx - 8, cy - 8, cx + 8, cy + 8], outline=(100, 200, 255), width=2)
    # Plus
    draw.line([cx - 4, cy, cx + 4, cy], fill=(100, 200, 255), width=2)
    draw.line([cx, cy - 4, cx, cy + 4], fill=(100, 200, 255), width=2)
    # Larger circle hint
    draw.arc([cx - 15, cy - 15, cx + 15, cy + 15], 0, 360, fill=(60, 120, 160), width=1)

img = make_button("SIZE", "hold: down", (30, 40, 60), (15, 22, 35), (60, 120, 180), (100, 200, 255), draw_size_up)
img.save(os.path.join(OUT, "size-up.png"))
print("  ✓ size-up.png")

# ── Color cycle ────────────────────────────────────────────────────────────────
def draw_color_wheel(draw):
    import math
    cx, cy = 36, 28
    colors = [(255, 0, 0), (255, 170, 0), (255, 255, 0), (0, 255, 0), (0, 130, 255), (200, 0, 255)]
    for i, c in enumerate(colors):
        angle = i * 60 - 90
        x = cx + int(12 * math.cos(math.radians(angle)))
        y = cy + int(12 * math.sin(math.radians(angle)))
        draw.ellipse([x - 4, y - 4, x + 4, y + 4], fill=c)

img = make_button("COLOR", "hold: prev", (50, 30, 50), (30, 15, 30), (180, 60, 180), (255, 150, 255), draw_color_wheel)
img.save(os.path.join(OUT, "color.png"))
print("  ✓ color.png")

# ── Opacity up ─────────────────────────────────────────────────────────────────
def draw_opacity(draw):
    cx, cy = 36, 28
    for i in range(4):
        alpha = 60 + i * 60
        x = 14 + i * 14
        draw.ellipse([x - 5, cy - 5, x + 5, cy + 5], fill=(255, 200, 0, alpha))

img = make_button("ALPHA", "hold: down", (50, 45, 20), (30, 25, 10), (200, 160, 40), (255, 220, 80), draw_opacity)
img.save(os.path.join(OUT, "opacity.png"))
print("  ✓ opacity.png")

# ── Exit ───────────────────────────────────────────────────────────────────────
img = make_button("EXIT", None, (60, 60, 60), (30, 30, 30), (120, 120, 120), (255, 100, 100))
img.save(os.path.join(OUT, "exit.png"))
print("  ✓ exit.png")

# ── Status init (off state) ───────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(25, 10, 10), outline=(120, 40, 40), width=2)
draw.text((20, 4), "OFF", fill=(255, 60, 60), font=FONT_LG)
draw.ellipse([24, 30, 48, 54], fill=(255, 68, 68, 150))
draw.text((8, 58), "r=20", fill=(180, 180, 180), font=FONT_SM)
img.save(os.path.join(OUT, "status-init.png"))
print("  ✓ status-init.png")

# ── Main deck icon ─────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (40, 30, 50), (20, 15, 30))
draw.rounded_rectangle([6, 6, 66, 66], radius=10, fill=(30, 20, 40), outline=(140, 80, 180), width=2)

# Mouse cursor shape
cursor_pts = [(20, 12), (20, 42), (28, 36), (36, 48), (40, 46), (32, 34), (40, 32), (20, 12)]
draw.polygon(cursor_pts, fill=(220, 220, 220), outline=(60, 60, 60))

# Highlight circle
draw.ellipse([22, 20, 56, 54], outline=(255, 80, 80, 200), width=3)
draw.ellipse([26, 24, 52, 50], outline=(255, 120, 60, 130), width=2)

draw.text((8, 56), "MOUSE", fill=(200, 160, 255), font=FONT_SM)
img.save(os.path.join(OUT, "highlight-icon.png"))
print("  ✓ highlight-icon.png")

print("\nAll highlight assets generated!")
