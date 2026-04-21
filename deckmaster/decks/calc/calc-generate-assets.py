#!/usr/bin/env python3
"""Generate calculator button images (72x72 PNG) for Stream Deck."""

import os
from PIL import Image, ImageDraw, ImageFont

SIZE = 72
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
os.makedirs(OUT, exist_ok=True)

try:
    FONT_XL = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 32)
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 22)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 16)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
except:
    FONT_XL = FONT_LG = FONT_MD = FONT_SM = ImageFont.load_default()


def gradient(draw, size, c_top, c_bot):
    for y in range(size):
        t = y / size
        r = int(c_top[0] + (c_bot[0] - c_top[0]) * t)
        g = int(c_top[1] + (c_bot[1] - c_top[1]) * t)
        b = int(c_top[2] + (c_bot[2] - c_top[2]) * t)
        draw.line([(0, y), (size - 1, y)], fill=(r, g, b))


def make_button(label, bg_top, bg_bot, text_color, outline_color, font=None, sublabel=None):
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    gradient(draw, SIZE, bg_top, bg_bot)
    draw.rounded_rectangle([3, 3, 69, 69], radius=10, outline=outline_color, width=2)

    if font is None:
        font = FONT_XL

    bbox = draw.textbbox((0, 0), label, font=font)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
    y_off = -4 if sublabel else 0
    draw.text(((SIZE - tw) // 2, (SIZE - th) // 2 + y_off), label, fill=text_color, font=font)

    if sublabel:
        bbox2 = draw.textbbox((0, 0), sublabel, font=FONT_SM)
        tw2 = bbox2[2] - bbox2[0]
        draw.text(((SIZE - tw2) // 2, 54), sublabel, fill=(180, 180, 180), font=FONT_SM)

    return img


# ── Digit buttons ──────────────────────────────────────────────────────────────
for d in range(10):
    img = make_button(str(d),
                      bg_top=(50, 50, 65), bg_bot=(30, 30, 42),
                      text_color=(255, 255, 255),
                      outline_color=(80, 80, 100))
    img.save(os.path.join(OUT, f"digit-{d}.png"))
    print(f"  ✓ digit-{d}.png")

# ── Operator buttons ───────────────────────────────────────────────────────────
ops = {
    "plus":     ("+",  "hold: −"),
    "minus":    ("−",  None),
    "multiply": ("×",  "hold: ÷"),
    "divide":   ("÷",  None),
}
for name, (sym, sub) in ops.items():
    img = make_button(sym,
                      bg_top=(180, 100, 20), bg_bot=(120, 60, 10),
                      text_color=(255, 255, 255),
                      outline_color=(220, 140, 40),
                      sublabel=sub)
    img.save(os.path.join(OUT, f"op-{name}.png"))
    print(f"  ✓ op-{name}.png")

# ── Equals / Clear ─────────────────────────────────────────────────────────────
img = make_button("=",
                  bg_top=(0, 140, 60), bg_bot=(0, 90, 35),
                  text_color=(255, 255, 255),
                  outline_color=(0, 200, 80),
                  sublabel="hold: C")
img.save(os.path.join(OUT, "op-equals.png"))
print("  ✓ op-equals.png")

img = make_button("C",
                  bg_top=(160, 30, 30), bg_bot=(100, 15, 15),
                  text_color=(255, 255, 255),
                  outline_color=(220, 60, 60))
img.save(os.path.join(OUT, "op-clear.png"))
print("  ✓ op-clear.png")

# ── Exit button ────────────────────────────────────────────────────────────────
img = make_button("EXIT",
                  bg_top=(60, 60, 60), bg_bot=(30, 30, 30),
                  text_color=(255, 100, 100),
                  outline_color=(120, 120, 120),
                  font=FONT_MD)
img.save(os.path.join(OUT, "exit.png"))
print("  ✓ exit.png")

# ── Display (empty) ───────────────────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (15, 25, 15), (5, 12, 5))
draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(40, 80, 40), width=2)
draw.text((50, 26), "0", fill=(0, 255, 80), font=FONT_XL)
img.save(os.path.join(OUT, "display-init.png"))
print("  ✓ display-init.png")

# ── Calculator icon for main deck ─────────────────────────────────────────────
img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
gradient(draw, SIZE, (40, 40, 60), (20, 20, 35))
draw.rounded_rectangle([6, 6, 66, 66], radius=10, fill=(30, 30, 45), outline=(100, 100, 140), width=2)

# Screen area
draw.rounded_rectangle([10, 10, 62, 28], radius=4, fill=(10, 30, 10), outline=(40, 80, 40), width=1)
draw.text((36, 10), "42", fill=(0, 255, 80), font=FONT_MD)

# Button grid
colors = [(80, 80, 100), (180, 100, 20), (0, 140, 60)]
for row in range(3):
    for col in range(4):
        x = 12 + col * 14
        y = 33 + row * 12
        c = colors[0] if col < 3 else colors[1]
        if row == 2 and col == 3:
            c = colors[2]
        draw.rounded_rectangle([x, y, x + 10, y + 9], radius=2, fill=c)

draw.text((14, 60), "CALC", fill=(200, 200, 255), font=FONT_SM)
img.save(os.path.join(OUT, "calc-icon.png"))
print("  ✓ calc-icon.png")

print("\nAll calculator assets generated!")
