#!/usr/bin/env python3
"""Generate Polymarket category button assets."""

import os
from PIL import Image, ImageDraw, ImageFont

SIZE = 72
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
os.makedirs(OUT, exist_ok=True)

try:
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 9)
except Exception:
    FONT_MD = FONT_SM = ImageFont.load_default()


def gradient(draw, size, c_top, c_bot):
    for y in range(size):
        t = y / size
        r = int(c_top[0] + (c_bot[0] - c_top[0]) * t)
        g = int(c_top[1] + (c_bot[1] - c_top[1]) * t)
        b = int(c_top[2] + (c_bot[2] - c_top[2]) * t)
        draw.line([(0, y), (size - 1, y)], fill=(r, g, b))


# Category button showing current category name
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (35, 25, 50), (18, 12, 28))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(100, 60, 160), width=2)
draw.text((12, 10), "FILTER", fill=(180, 140, 220), font=FONT_MD)
draw.text((10, 28), "press to", fill=(120, 100, 150), font=FONT_SM)
draw.text((10, 40), "change", fill=(120, 100, 150), font=FONT_SM)
# Small arrows
draw.polygon([(52, 52), (60, 52), (56, 46)], fill=(140, 100, 180))
draw.polygon([(52, 56), (60, 56), (56, 62)], fill=(140, 100, 180))
img.save(os.path.join(OUT, "category.png"))
print("  ✓ category.png")

print("Done!")
