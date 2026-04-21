#!/bin/bash
# Generate a door status button image:
#   - Small MDI door icon top-centre
#   - Large time label (e.g. "2h" or "OPEN") below it
#   - Small location name at the bottom
# Usage: ha-door-icon.sh <entity_id> <cache_name> <label>

CACHE="/tmp/streamdeck-ha"
mkdir -p "$CACHE"

ENTITY="$1"
ICON_NAME="$2"
LABEL="$3"
OUT="$CACHE/door-${ICON_NAME}.png"
SVG_OPEN="$CACHE/mdi-door-open.svg"
SVG_CLOSED="$CACHE/mdi-door-closed.svg"

# ── Ensure MDI SVGs are cached ────────────────────────────────────────────────
if [[ ! -f "$SVG_OPEN" ]]; then
    curl -s "https://api.iconify.design/mdi/door-open.svg" -o "$SVG_OPEN" 2>/dev/null
fi
if [[ ! -f "$SVG_CLOSED" ]]; then
    curl -s "https://api.iconify.design/mdi/door.svg" -o "$SVG_CLOSED" 2>/dev/null
fi

# ── Fetch HA state + last_changed ─────────────────────────────────────────────
read -r STATE TIME_STR < <(
    curl -s -H "Authorization: Bearer $HA_TOKEN" \
        "$HA_URL/api/states/$ENTITY" 2>/dev/null \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('state','off'), d.get('last_changed',''))
except:
    print('off', '')
" 2>/dev/null
)

# ── Compute time string ────────────────────────────────────────────────────────
TIME_LABEL=$(python3 -c "
import sys
from datetime import datetime, timezone

state, ts = '$STATE', '$TIME_STR'
if state == 'on':
    print('OPEN')
else:
    try:
        dt   = datetime.fromisoformat(ts.replace('Z', '+00:00'))
        secs = (datetime.now(timezone.utc) - dt).total_seconds()
        if secs < 60:
            print('<1m')
        elif secs < 3600:
            print(f'{int(secs//60)}m')
        elif secs < 86400:
            print(f'{int(secs//3600)}h')
        else:
            print(f'{int(secs//86400)}d')
    except Exception:
        print('?')
" 2>/dev/null)

# ── Render full 72×72 button image ────────────────────────────────────────────
python3 << PYEOF
import sys, subprocess, os, tempfile
from PIL import Image, ImageDraw, ImageFont

SIZE       = 72
FONT_PATH  = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_SMALL = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

state      = "$STATE"
time_label = "$TIME_LABEL"
label      = "$LABEL"
outpath    = "$OUT"
svg_open   = "$SVG_OPEN"
svg_closed = "$SVG_CLOSED"

is_open    = state == "on"
icon_color = "#ff5555" if is_open else "#55aaff"
time_color = (255, 85, 85, 255) if is_open else (200, 200, 200, 255)

# ── Render MDI SVG → PIL image via ImageMagick ────────────────────────────────
svg_path = svg_open if is_open else svg_closed
icon_img = None
try:
    with tempfile.NamedTemporaryFile(suffix=".svg", mode="w", delete=False) as f:
        with open(svg_path) as src:
            f.write(src.read().replace("currentColor", icon_color))
        tmp_svg = f.name
    tmp_png = tmp_svg.replace(".svg", ".png")
    subprocess.run(
        ["convert", "-background", "none", "-resize", "22x22", "svg:" + tmp_svg, tmp_png],
        capture_output=True
    )
    icon_img = Image.open(tmp_png).convert("RGBA")
    os.unlink(tmp_svg)
    os.unlink(tmp_png)
except Exception:
    pass

# ── Fonts ─────────────────────────────────────────────────────────────────────
try:    font_big = ImageFont.truetype(FONT_PATH, 24)
except: font_big = ImageFont.load_default()
try:    font_sm  = ImageFont.truetype(FONT_SMALL, 10)
except: font_sm  = ImageFont.load_default()

# ── Canvas ────────────────────────────────────────────────────────────────────
img  = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))
draw = ImageDraw.Draw(img)

# icon — small, top-centre
if icon_img:
    iw, ih = icon_img.size
    img.paste(icon_img, ((SIZE - iw) // 2, 4), icon_img)

# large time — centred below icon
bbox = draw.textbbox((0, 0), time_label, font=font_big)
tw = bbox[2] - bbox[0]
draw.text(((SIZE - tw) // 2, 28), time_label, font=font_big, fill=time_color)

# small name — bottom
bbox2 = draw.textbbox((0, 0), label, font=font_sm)
lw = bbox2[2] - bbox2[0]
draw.text(((SIZE - lw) // 2, SIZE - 13), label, font=font_sm, fill=(160, 160, 180, 255))

img.save(outpath)
PYEOF

echo "$OUT"
