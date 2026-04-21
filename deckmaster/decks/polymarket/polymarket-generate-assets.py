#!/usr/bin/env python3
"""Generate Polymarket deck button assets."""

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


# ── Next page (▶) ─────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (20, 30, 50), (10, 15, 30))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(60, 100, 180), width=2)
# Arrow
draw.polygon([(28, 20), (50, 36), (28, 52)], fill=(80, 150, 255))
draw.text((22, 54), "NEXT", fill=(100, 160, 255), font=FONT_XS)
img.save(os.path.join(OUT, "next.png"))
print("  ✓ next.png")

# ── Prev page (◀) ─────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (20, 30, 50), (10, 15, 30))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(60, 100, 180), width=2)
draw.polygon([(44, 20), (22, 36), (44, 52)], fill=(80, 150, 255))
draw.text((22, 54), "PREV", fill=(100, 160, 255), font=FONT_XS)
img.save(os.path.join(OUT, "prev.png"))
print("  ✓ prev.png")

# ── Reload ─────────────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (30, 40, 25), (15, 22, 12))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(60, 140, 60), width=2)
# Circular arrow (simplified)
draw.arc([18, 16, 54, 52], start=30, end=330, fill=(80, 220, 80), width=3)
draw.polygon([(50, 20), (54, 30), (44, 28)], fill=(80, 220, 80))
draw.text((8, 56), "RELOAD", fill=(80, 200, 80), font=FONT_XS)
img.save(os.path.join(OUT, "reload.png"))
print("  ✓ reload.png")

# ── Exit ───────────────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (50, 50, 50), (25, 25, 25))
draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=(120, 120, 120), width=2)
bbox = draw.textbbox((0, 0), "EXIT", font=FONT_MD)
tw = bbox[2] - bbox[0]
draw.text(((SIZE - tw) // 2, (SIZE - 14) // 2), "EXIT", fill=(255, 100, 100), font=FONT_MD)
img.save(os.path.join(OUT, "exit.png"))
print("  ✓ exit.png")

# ── Main deck icon ─────────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (15, 25, 50), (8, 12, 30))
draw.rounded_rectangle([6, 6, 66, 66], radius=10, fill=(12, 20, 45), outline=(60, 100, 200), width=2)

# Mini chart bars
colors = [(30, 180, 60), (180, 160, 30), (180, 40, 40), (30, 140, 200)]
heights = [30, 22, 18, 26]
for i, (c, h) in enumerate(zip(colors, heights)):
    x = 14 + i * 13
    draw.rectangle([x, 48 - h, x + 9, 48], fill=c)

# Percentages
draw.text((14, 10), "72%", fill=(60, 255, 100), font=FONT_SM)
draw.text((40, 10), "34%", fill=(255, 80, 80), font=FONT_SM)

draw.text((14, 54), "POLY", fill=(140, 180, 255), font=FONT_SM)

img.save(os.path.join(OUT, "polymarket-icon.png"))
print("  ✓ polymarket-icon.png")

# ── Info display init ──────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(15, 15, 30), outline=(60, 80, 140), width=2)
draw.text((18, 8), "1/?", fill=(100, 160, 255), font=FONT_LG)
draw.text((8, 32), "TRENDING", fill=(140, 140, 180), font=FONT_SM)
draw.text((10, 44), "Polymarket", fill=(80, 80, 120), font=FONT_XS)
draw.text((8, 58), "loading..", fill=(100, 100, 140), font=FONT_XS)
img.save(os.path.join(OUT, "info-init.png"))
print("  ✓ info-init.png")

print("\nAll polymarket assets generated!")
