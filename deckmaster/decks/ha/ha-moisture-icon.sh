#!/bin/bash
# Generate a plant moisture button image:
#   - Small MDI sprout icon + vertical bar graph (top area)
#   - Large moisture percentage below
#   - Small plant name at the bottom
# Color: orange < 25% (dry) → green 25–65% (ok) → blue > 65% (wet)
# Usage: ha-moisture-icon.sh <entity_id> <cache_name> <label>

CACHE="/tmp/streamdeck-ha"
mkdir -p "$CACHE"

ENTITY="$1"
ICON_NAME="$2"
LABEL="$3"
OUT="$CACHE/plant-${ICON_NAME}.png"
SVG_PLANT="$CACHE/mdi-sprout.svg"

# ── Ensure MDI SVG is cached ──────────────────────────────────────────────────
if [[ ! -f "$SVG_PLANT" ]]; then
    curl -s "https://api.iconify.design/mdi/sprout.svg" -o "$SVG_PLANT" 2>/dev/null
fi

# ── Fetch moisture value from HA ─────────────────────────────────────────────
MOISTURE=$(curl -s -H "Authorization: Bearer $HA_TOKEN" \
    "$HA_URL/api/states/$ENTITY" 2>/dev/null \
    | python3 -c "
import sys, json
try: print(float(json.load(sys.stdin).get('state', 0)))
except: print(0.0)
" 2>/dev/null)

# ── Render full 72×72 button image ────────────────────────────────────────────
python3 << PYEOF
import sys, subprocess, os, tempfile
from PIL import Image, ImageDraw, ImageFont

SIZE       = 72
FONT_PATH  = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_SMALL = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

try:    moisture = float("$MOISTURE")
except: moisture = 0.0

label   = "$LABEL"
outpath = "$OUT"
svg     = "$SVG_PLANT"

# ── Colour by moisture level ──────────────────────────────────────────────────
if moisture < 25:
    icon_color = "#dd6622"          # dry  → orange
    time_color = (220, 100, 30, 255)
elif moisture < 65:
    icon_color = "#44cc55"          # ok   → green
    time_color = (70, 200, 80, 255)
else:
    icon_color = "#4499ee"          # wet  → blue
    time_color = (60, 140, 220, 255)

# ── Render MDI sprout SVG ─────────────────────────────────────────────────────
icon_img = None
try:
    with tempfile.NamedTemporaryFile(suffix=".svg", mode="w", delete=False) as f:
        with open(svg) as src:
            f.write(src.read().replace("currentColor", icon_color))
        tmp_svg = f.name
    tmp_png = tmp_svg.replace(".svg", ".png")
    subprocess.run(
        ["convert", "-background", "none", "-resize", "20x20", "svg:" + tmp_svg, tmp_png],
        capture_output=True
    )
    icon_img = Image.open(tmp_png).convert("RGBA")
    os.unlink(tmp_svg)
    os.unlink(tmp_png)
except Exception:
    pass

# ── Fonts ─────────────────────────────────────────────────────────────────────
try:    font_big = ImageFont.truetype(FONT_PATH, 22)
except: font_big = ImageFont.load_default()
try:    font_sm  = ImageFont.truetype(FONT_SMALL, 10)
except: font_sm  = ImageFont.load_default()

# ── Canvas ────────────────────────────────────────────────────────────────────
img  = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))
draw = ImageDraw.Draw(img)

# ── Top row: icon (left) + bar graph (right) ──────────────────────────────────
TOP_Y      = 4
ICON_W     = 20
BAR_X      = 4 + ICON_W + 4   # just right of the icon
BAR_W      = 8
BAR_H      = 20
BAR_Y_TOP  = TOP_Y
BAR_Y_BOT  = TOP_Y + BAR_H

# icon
if icon_img:
    img.paste(icon_img, (4, TOP_Y), icon_img)

# bar graph background
bar_bg = (45, 45, 60, 255)
draw.rectangle([BAR_X, BAR_Y_TOP, BAR_X + BAR_W, BAR_Y_BOT], fill=bar_bg)

# bar fill
fill_h = int(BAR_H * min(max(moisture, 0), 100) / 100)
if fill_h > 0:
    bar_fill = tuple(int(c * 0.85) for c in time_color[:3]) + (255,)
    draw.rectangle(
        [BAR_X + 1, BAR_Y_BOT - fill_h, BAR_X + BAR_W - 1, BAR_Y_BOT - 1],
        fill=bar_fill
    )

# ── Large percentage — centred below top row ──────────────────────────────────
pct_str = f"{int(moisture)}%"
bbox = draw.textbbox((0, 0), pct_str, font=font_big)
tw = bbox[2] - bbox[0]
draw.text(((SIZE - tw) // 2, 27), pct_str, font=font_big, fill=time_color)

# ── Small name at bottom ──────────────────────────────────────────────────────
bbox2 = draw.textbbox((0, 0), label, font=font_sm)
lw = bbox2[2] - bbox2[0]
draw.text(((SIZE - lw) // 2, SIZE - 13), label, font=font_sm, fill=(160, 160, 180, 255))

img.save(outpath)
PYEOF

echo "$OUT"
