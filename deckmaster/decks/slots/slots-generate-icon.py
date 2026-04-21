#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""Generate the slot machine button icon for the main deck."""
from PIL import Image, ImageDraw, ImageFont
import math, random

SIZE = 72
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)

# Purple gradient background
for y in range(SIZE):
    t = y / SIZE
    r = int(60 + 40 * t)
    g = int(0 + 10 * t)
    b = int(100 - 30 * t)
    draw.line([(0, y), (SIZE - 1, y)], fill=(r, g, b))

# Slot machine body
draw.rounded_rectangle([8, 12, 64, 60], radius=6, fill=(40, 0, 70), outline=(200, 150, 255), width=2)

# Three reel windows
for i, color in enumerate([(255, 50, 50), (255, 220, 40), (60, 200, 60)]):
    x = 14 + i * 17
    draw.rounded_rectangle([x, 20, x + 13, 40], radius=3, fill=(20, 0, 40), outline=color, width=2)

try:
    font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
    font_sm = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 7)
except:
    font = font_sm = ImageFont.load_default()

# "7" symbols in windows
for i, sym in enumerate(["7", "★", "7"]):
    x = 17 + i * 17
    draw.text((x, 22), sym, fill=(255, 215, 0), font=font)

# "SLOTS" label
draw.text((16, 44), "SLOTS", fill=(255, 215, 0), font=font)

# Lever
draw.line([(62, 18), (66, 10)], fill=(200, 200, 200), width=3)
draw.ellipse([63, 6, 70, 13], fill=(255, 50, 50))

# Sparkles
random.seed(42)
for _ in range(5):
    x = random.randint(4, 68)
    y = random.randint(4, 68)
    draw.ellipse([x-1, y-1, x+1, y+1], fill=(255, 255, 255, 200))

img.save(os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets", "slots-icon.png"))
print("✓ slots-icon.png")
