#!/usr/bin/env python3
"""Generate colorful slot machine symbol images (72x72 PNG) for Stream Deck."""

import math
import os
import random
from PIL import Image, ImageDraw, ImageFont, ImageFilter

SIZE = 72
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
os.makedirs(OUT, exist_ok=True)

def gradient_bg(draw, size, color_top, color_bot):
    """Draw a vertical gradient background."""
    for y in range(size):
        t = y / size
        r = int(color_top[0] + (color_bot[0] - color_top[0]) * t)
        g = int(color_top[1] + (color_bot[1] - color_top[1]) * t)
        b = int(color_top[2] + (color_bot[2] - color_top[2]) * t)
        draw.line([(0, y), (size - 1, y)], fill=(r, g, b))

def add_sparkles(draw, size, count=6):
    """Add small sparkle dots."""
    for _ in range(count):
        x = random.randint(4, size - 5)
        y = random.randint(4, size - 5)
        r = random.randint(1, 2)
        alpha = random.randint(180, 255)
        draw.ellipse([x-r, y-r, x+r, y+r], fill=(255, 255, 255, alpha))

def draw_cherry(img, draw):
    gradient_bg(draw, SIZE, (60, 0, 30), (20, 0, 10))
    # Two cherries
    draw.ellipse([14, 30, 34, 50], fill=(220, 20, 20), outline=(180, 10, 10), width=2)
    draw.ellipse([38, 30, 58, 50], fill=(230, 30, 30), outline=(180, 10, 10), width=2)
    # Highlights
    draw.ellipse([18, 34, 24, 40], fill=(255, 120, 120))
    draw.ellipse([42, 34, 48, 40], fill=(255, 120, 120))
    # Stems
    draw.arc([20, 8, 50, 38], start=200, end=320, fill=(40, 140, 40), width=3)
    draw.arc([24, 8, 54, 38], start=220, end=340, fill=(40, 140, 40), width=3)
    # Leaf
    draw.ellipse([30, 10, 44, 20], fill=(60, 180, 60))
    add_sparkles(draw, SIZE)

def draw_lemon(img, draw):
    gradient_bg(draw, SIZE, (50, 50, 0), (20, 20, 0))
    # Lemon body
    draw.ellipse([12, 16, 60, 56], fill=(255, 220, 40), outline=(200, 170, 20), width=2)
    # Highlight
    draw.ellipse([20, 22, 38, 38], fill=(255, 245, 120))
    # Lemon tips
    draw.polygon([(12, 36), (6, 33), (8, 39)], fill=(255, 220, 40))
    draw.polygon([(60, 36), (66, 33), (64, 39)], fill=(255, 220, 40))
    add_sparkles(draw, SIZE)

def draw_bell(img, draw):
    gradient_bg(draw, SIZE, (50, 40, 0), (20, 15, 0))
    # Bell body
    draw.polygon([
        (36, 10), (52, 30), (56, 48), (16, 48), (20, 30)
    ], fill=(255, 200, 40), outline=(200, 150, 20), width=2)
    # Bell rim
    draw.rectangle([12, 48, 60, 56], fill=(255, 180, 30), outline=(200, 140, 10), width=2)
    # Clapper
    draw.ellipse([31, 54, 41, 64], fill=(200, 150, 20))
    # Highlight
    draw.polygon([(30, 16), (36, 14), (38, 30), (28, 30)], fill=(255, 235, 130))
    add_sparkles(draw, SIZE)

def draw_diamond(img, draw):
    gradient_bg(draw, SIZE, (0, 20, 80), (0, 5, 30))
    cx, cy = 36, 36
    # Diamond shape
    pts = [(cx, cy - 24), (cx + 20, cy), (cx, cy + 24), (cx - 20, cy)]
    draw.polygon(pts, fill=(60, 140, 255), outline=(40, 100, 220), width=2)
    # Facets
    draw.polygon([(cx, cy - 24), (cx + 8, cy - 6), (cx - 8, cy - 6)], fill=(120, 190, 255))
    draw.polygon([(cx, cy - 24), (cx + 20, cy), (cx + 8, cy - 6)], fill=(80, 160, 255))
    draw.line([(cx, cy - 24), (cx, cy + 24)], fill=(100, 180, 255), width=1)
    draw.line([(cx - 20, cy), (cx + 20, cy)], fill=(100, 180, 255), width=1)
    add_sparkles(draw, SIZE, 8)

def draw_seven(img, draw):
    gradient_bg(draw, SIZE, (60, 0, 0), (30, 0, 0))
    try:
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 52)
    except:
        font = ImageFont.load_default()
    # Shadow
    draw.text((20, 6), "7", fill=(100, 0, 0), font=font)
    # Main
    draw.text((18, 4), "7", fill=(255, 50, 50), font=font)
    # Gold outline effect
    draw.text((18, 4), "7", fill=None, font=font)
    add_sparkles(draw, SIZE, 8)

def draw_star(img, draw):
    gradient_bg(draw, SIZE, (50, 40, 0), (20, 10, 0))
    cx, cy = 36, 38
    r_out, r_in = 24, 10
    pts = []
    for i in range(10):
        angle = math.radians(i * 36 - 90)
        r = r_out if i % 2 == 0 else r_in
        pts.append((cx + r * math.cos(angle), cy + r * math.sin(angle)))
    draw.polygon(pts, fill=(255, 215, 0), outline=(200, 160, 0), width=2)
    # Inner highlight
    inner_pts = []
    for i in range(10):
        angle = math.radians(i * 36 - 90)
        r = (r_out * 0.6) if i % 2 == 0 else (r_in * 0.6)
        inner_pts.append((cx + r * math.cos(angle), cy + r * math.sin(angle)))
    draw.polygon(inner_pts, fill=(255, 240, 120))
    add_sparkles(draw, SIZE, 8)

def draw_bar(img, draw):
    gradient_bg(draw, SIZE, (30, 0, 50), (10, 0, 20))
    # BAR background
    draw.rounded_rectangle([6, 20, 66, 52], radius=6, fill=(180, 0, 220), outline=(140, 0, 180), width=2)
    # Gold inner
    draw.rounded_rectangle([10, 24, 62, 48], radius=4, fill=(80, 0, 120))
    try:
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 22)
    except:
        font = ImageFont.load_default()
    bbox = draw.textbbox((0, 0), "BAR", font=font)
    tw = bbox[2] - bbox[0]
    th = bbox[3] - bbox[1]
    draw.text(((72 - tw) // 2, (72 - th) // 2 - 2), "BAR", fill=(255, 215, 0), font=font)
    add_sparkles(draw, SIZE)

# --- UI buttons ---

def draw_spin_button():
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    gradient_bg(draw, SIZE, (0, 100, 0), (0, 50, 0))
    draw.rounded_rectangle([4, 4, 68, 68], radius=12, fill=(0, 200, 50), outline=(0, 255, 80), width=3)
    try:
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 20)
    except:
        font = ImageFont.load_default()
    bbox = draw.textbbox((0, 0), "SPIN", font=font)
    tw = bbox[2] - bbox[0]
    draw.text(((72 - tw) // 2, 22), "SPIN", fill=(255, 255, 255), font=font)
    return img

def draw_back_button():
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    gradient_bg(draw, SIZE, (60, 60, 60), (30, 30, 30))
    draw.rounded_rectangle([4, 4, 68, 68], radius=12, fill=(80, 80, 80), outline=(120, 120, 120), width=2)
    try:
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    except:
        font = ImageFont.load_default()
    bbox = draw.textbbox((0, 0), "EXIT", font=font)
    tw = bbox[2] - bbox[0]
    draw.text(((72 - tw) // 2, 26), "EXIT", fill=(255, 100, 100), font=font)
    return img

def draw_credits_bg():
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    gradient_bg(draw, SIZE, (0, 0, 40), (0, 0, 15))
    draw.rounded_rectangle([2, 2, 70, 70], radius=8, outline=(60, 60, 120), width=2)
    return img

def draw_spinning_blur():
    """A blurry/streaky frame for spinning animation."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    gradient_bg(draw, SIZE, (20, 20, 30), (10, 10, 15))
    # Horizontal streaks
    for y in range(0, SIZE, 3):
        c = random.randint(40, 120)
        draw.line([(8, y), (64, y)], fill=(c, c, c + 30), width=1)
    return img

def draw_win_frame(symbol_img):
    """Add golden glowing border to a winning symbol."""
    img = symbol_img.copy()
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([1, 1, 71, 71], radius=4, outline=(255, 215, 0), width=4)
    draw.rounded_rectangle([4, 4, 68, 68], radius=3, outline=(255, 255, 150), width=2)
    add_sparkles(draw, SIZE, 12)
    return img

def draw_glitter_frame(n):
    """Generate a glitter overlay frame."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    random.seed(n * 42)
    for _ in range(20):
        x = random.randint(0, SIZE - 1)
        y = random.randint(0, SIZE - 1)
        r = random.randint(1, 3)
        colors = [(255, 215, 0), (255, 255, 150), (255, 180, 0), (255, 255, 255), (255, 100, 100), (100, 255, 100), (100, 200, 255)]
        c = random.choice(colors)
        draw.ellipse([x-r, y-r, x+r, y+r], fill=c + (random.randint(150, 255),))
    return img

# Generate all symbols
SYMBOLS = {
    "cherry": draw_cherry,
    "lemon": draw_lemon,
    "bell": draw_bell,
    "diamond": draw_diamond,
    "seven": draw_seven,
    "star": draw_star,
    "bar": draw_bar,
}

random.seed(12345)

for name, func in SYMBOLS.items():
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    func(img, draw)
    img.save(os.path.join(OUT, f"{name}.png"))
    print(f"  ✓ {name}.png")

    # Win variant
    win_img = draw_win_frame(img)
    win_img.save(os.path.join(OUT, f"{name}-win.png"))
    print(f"  ✓ {name}-win.png")

# Spin blur frames (for animation)
for i in range(4):
    random.seed(i * 77)
    img = draw_spinning_blur()
    img.save(os.path.join(OUT, f"spin-blur-{i}.png"))
    print(f"  ✓ spin-blur-{i}.png")

# Glitter frames
for i in range(6):
    img = draw_glitter_frame(i)
    img.save(os.path.join(OUT, f"glitter-{i}.png"))
    print(f"  ✓ glitter-{i}.png")

# UI buttons
draw_spin_button().save(os.path.join(OUT, "spin-btn.png"))
print("  ✓ spin-btn.png")

draw_back_button().save(os.path.join(OUT, "exit-btn.png"))
print("  ✓ exit-btn.png")

draw_credits_bg().save(os.path.join(OUT, "credits-bg.png"))
print("  ✓ credits-bg.png")

# Jackpot / Big Win image
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
gradient_bg(draw, SIZE, (80, 60, 0), (40, 20, 0))
draw.rounded_rectangle([3, 3, 69, 69], radius=8, fill=(60, 40, 0), outline=(255, 215, 0), width=3)
try:
    font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    font_sm = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
except:
    font = font_sm = ImageFont.load_default()
draw.text((14, 16), "YOU", fill=(255, 255, 100), font=font_sm)
draw.text((12, 32), "WIN!", fill=(255, 215, 0), font=font)
add_sparkles(draw, SIZE, 12)
img.save(os.path.join(OUT, "you-win.png"))
print("  ✓ you-win.png")

# Slot machine frame / empty cell
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
gradient_bg(draw, SIZE, (15, 15, 25), (5, 5, 10))
draw.rounded_rectangle([2, 2, 70, 70], radius=4, outline=(40, 40, 60), width=2)
img.save(os.path.join(OUT, "cell-empty.png"))
print("  ✓ cell-empty.png")

print("\nAll slot assets generated!")
