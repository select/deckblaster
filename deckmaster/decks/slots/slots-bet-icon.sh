#!/bin/bash
# Renders current bet as a PNG icon for the BET button.
# Read from state file, generate image via Python one-liner.
STATE=/tmp/streamdeck-slots.json
IMG=/tmp/streamdeck-slots/bet-display.png

BET=5
if [ -f "$STATE" ]; then
    BET=$(python3 -c "import json; print(json.load(open('$STATE')).get('bet', 5))" 2>/dev/null || echo 5)
fi

python3 -c "
from PIL import Image, ImageDraw, ImageFont
img = Image.new('RGBA', (72, 72), (10, 10, 30, 255))
draw = ImageDraw.Draw(img)
draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(100, 60, 60), width=2)
sm = ImageFont.truetype('/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf', 11)
xl = ImageFont.truetype('/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf', 28)
draw.text((10, 6), 'BET ▲', fill=(200, 120, 120), font=sm)
t = '$BET'
bb = draw.textbbox((0,0), t, font=xl)
draw.text(((72-(bb[2]-bb[0]))//2, 30), t, fill=(255, 150, 80), font=xl)
img.save('$IMG')
"

echo "$IMG"
