#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""Home Assistant Stream Deck controls.

Usage:
  ha.py icon-light   <entity> <icon_base>          — copy on/off room icon, print path
  ha.py icon-door    <entity> <cache_name> <label>  — render door status button, print path
  ha.py icon-moisture <entity> <cache_name> <label> — render plant moisture button, print path
  ha.py icon-bell    <entity> <cache_name> <label>  — render doorbell last-rung button, print path
  ha.py icon-camera  <entity> <cache_name> <label>  — render camera snapshot button, print path
  ha.py toggle-switch <entity>                      — toggle HA switch via REST
  ha.py toggle-light  <entity>                      — toggle HA light via REST
  ha.py icon-mower-badge <entity>                   — render HA icon with red mower badge on error
  ha.py poll-doors                                  — daemon: push alert on door open

Config (from environment, set by start.sh loading ~/.config/streamdeck.env):
  HA_URL      e.g. http://homeassistant.local:8123
  HA_TOKEN    long-lived access token
  DECK_API    e.g. http://localhost:9990
"""
import os
import sys
import json
import time
import subprocess
import tempfile
import urllib.request
from pathlib import Path

HA_URL   = os.environ.get("HA_URL",   "http://homeassistant.local:8123")
HA_TOKEN = os.environ.get("HA_TOKEN", "")
DECK_API = os.environ.get("DECK_API", "http://localhost:9990")

ASSETS = Path(__file__).parent / "assets"
CACHE  = Path("/tmp/streamdeck-ha")
CACHE.mkdir(exist_ok=True)

FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_REG  = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

# ── HA REST helpers ───────────────────────────────────────────────────────────

def ha_get(path):
    req = urllib.request.Request(
        f"{HA_URL}{path}",
        headers={"Authorization": f"Bearer {HA_TOKEN}"},
    )
    with urllib.request.urlopen(req, timeout=5) as r:
        return json.loads(r.read())

def ha_post(path, payload):
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        f"{HA_URL}{path}", data=data,
        headers={"Authorization": f"Bearer {HA_TOKEN}",
                 "Content-Type": "application/json"},
    )
    urllib.request.urlopen(req, timeout=5)

def get_state(entity):
    try:
        return ha_get(f"/api/states/{entity}").get("state", "?")
    except Exception:
        return "?"

def get_state_and_time(entity):
    try:
        d = ha_get(f"/api/states/{entity}")
        return d.get("state", "?"), d.get("last_changed", "")
    except Exception:
        return "?", ""

# ── SVG icon helpers ──────────────────────────────────────────────────────────

def fetch_mdi(name, dest: Path):
    """Download an MDI SVG from iconify if not already cached."""
    if not dest.exists():
        try:
            url = f"https://api.iconify.design/mdi/{name}.svg"
            urllib.request.urlretrieve(url, dest)
        except Exception:
            pass

def svg_to_pil(svg_path: Path, size: int, color: str):
    """Render an MDI SVG to a PIL Image via ImageMagick convert."""
    try:
        from PIL import Image
        with tempfile.NamedTemporaryFile(suffix=".svg", mode="w", delete=False) as f:
            f.write(svg_path.read_text().replace("currentColor", color))
            tmp_svg = f.name
        tmp_png = tmp_svg.replace(".svg", ".png")
        subprocess.run(
            ["convert", "-background", "none",
             "-resize", f"{size}x{size}", f"svg:{tmp_svg}", tmp_png],
            capture_output=True,
        )
        img = Image.open(tmp_png).convert("RGBA")
        os.unlink(tmp_svg)
        os.unlink(tmp_png)
        return img
    except Exception:
        return None

# ── icon-light ────────────────────────────────────────────────────────────────

def cmd_icon_light(entity, icon_base):
    """Fetch state, copy the matching on/off asset to cache, print path."""
    state = get_state(entity)
    variant = "on" if state == "on" else "off"
    src = ASSETS / f"{icon_base}-{variant}.png"
    dst = CACHE / f"{icon_base}.png"
    try:
        import shutil
        shutil.copy2(src, dst)
    except Exception:
        dst = src
    print(dst)

# ── icon-door ─────────────────────────────────────────────────────────────────

def cmd_icon_door(entity, cache_name, label):
    from PIL import Image, ImageDraw, ImageFont
    from datetime import datetime, timezone

    out = CACHE / f"door-{cache_name}.png"

    svg_open   = CACHE / "mdi-door-open.svg"
    svg_closed = CACHE / "mdi-door-closed.svg"
    fetch_mdi("door-open", svg_open)
    fetch_mdi("door",      svg_closed)

    state, time_str = get_state_and_time(entity)
    is_open = state == "on"

    # Compute time label
    if is_open:
        time_label = "OPEN"
    else:
        try:
            dt   = datetime.fromisoformat(time_str.replace("Z", "+00:00"))
            secs = (datetime.now(timezone.utc) - dt).total_seconds()
            if secs < 60:        time_label = "<1m"
            elif secs < 3600:    time_label = f"{int(secs//60)}m"
            elif secs < 86400:   time_label = f"{int(secs//3600)}h"
            else:                time_label = f"{int(secs//86400)}d"
        except Exception:
            time_label = "?"

    icon_color = "#ff5555" if is_open else "#55aaff"
    time_color = (255, 85, 85, 255) if is_open else (200, 200, 200, 255)

    svg_src  = svg_open if is_open else svg_closed
    icon_img = svg_to_pil(svg_src, 22, icon_color)

    try:    font_big = ImageFont.truetype(FONT_BOLD, 24)
    except: font_big = ImageFont.load_default()
    try:    font_sm  = ImageFont.truetype(FONT_REG, 10)
    except: font_sm  = ImageFont.load_default()

    SIZE = 72
    img  = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))
    draw = ImageDraw.Draw(img)

    if icon_img:
        iw, ih = icon_img.size
        img.paste(icon_img, ((SIZE - iw) // 2, 4), icon_img)

    bbox = draw.textbbox((0, 0), time_label, font=font_big)
    tw   = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 28), time_label, font=font_big, fill=time_color)

    bbox2 = draw.textbbox((0, 0), label, font=font_sm)
    lw    = bbox2[2] - bbox2[0]
    draw.text(((SIZE - lw) // 2, SIZE - 13), label, font=font_sm,
              fill=(160, 160, 180, 255))

    img.save(out)
    print(out)

# ── icon-moisture ─────────────────────────────────────────────────────────────

def cmd_icon_moisture(entity, cache_name, label):
    from PIL import Image, ImageDraw, ImageFont

    out      = CACHE / f"plant-{cache_name}.png"
    svg_file = CACHE / "mdi-sprout.svg"
    fetch_mdi("sprout", svg_file)

    try:
        moisture = float(get_state(entity))
    except Exception:
        moisture = 0.0

    if moisture < 25:
        icon_color = "#dd6622"
        time_color = (220, 100, 30, 255)
    elif moisture < 65:
        icon_color = "#44cc55"
        time_color = (70, 200, 80, 255)
    else:
        icon_color = "#4499ee"
        time_color = (60, 140, 220, 255)

    icon_img = svg_to_pil(svg_file, 20, icon_color)

    try:    font_big = ImageFont.truetype(FONT_BOLD, 22)
    except: font_big = ImageFont.load_default()
    try:    font_sm  = ImageFont.truetype(FONT_REG, 10)
    except: font_sm  = ImageFont.load_default()

    SIZE = 72
    img  = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))
    draw = ImageDraw.Draw(img)

    # Icon (top-left) + bar graph (beside it)
    if icon_img:
        img.paste(icon_img, (4, 4), icon_img)

    BAR_X, BAR_Y, BAR_W, BAR_H = 28, 4, 8, 20
    draw.rectangle([BAR_X, BAR_Y, BAR_X + BAR_W, BAR_Y + BAR_H],
                   fill=(45, 45, 60, 255))
    fill_h = int(BAR_H * min(max(moisture, 0), 100) / 100)
    if fill_h > 0:
        bar_fill = tuple(int(c * 0.85) for c in time_color[:3]) + (255,)
        draw.rectangle(
            [BAR_X + 1, BAR_Y + BAR_H - fill_h, BAR_X + BAR_W - 1, BAR_Y + BAR_H - 1],
            fill=bar_fill,
        )

    pct = f"{int(moisture)}%"
    bbox = draw.textbbox((0, 0), pct, font=font_big)
    tw   = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 27), pct, font=font_big, fill=time_color)

    bbox2 = draw.textbbox((0, 0), label, font=font_sm)
    lw    = bbox2[2] - bbox2[0]
    draw.text(((SIZE - lw) // 2, SIZE - 13), label, font=font_sm,
              fill=(160, 160, 180, 255))

    img.save(out)
    print(out)

# ── icon-bell ────────────────────────────────────────────────────────────────────

def cmd_icon_bell(entity, cache_name, label):
    """Render a doorbell button showing how long ago it last rang."""
    from PIL import Image, ImageDraw, ImageFont
    from datetime import datetime, timezone

    out      = CACHE / f"bell-{cache_name}.png"
    svg_file = CACHE / "mdi-bell.svg"
    fetch_mdi("bell", svg_file)

    state, time_str = get_state_and_time(entity)
    is_ringing = state == "on"

    if is_ringing:
        time_label = "NOW!"
        icon_color = "#ffdd00"
        time_color = (255, 220, 0, 255)
    else:
        icon_color = "#ffaa33"
        time_color = (210, 160, 80, 255)
        try:
            dt   = datetime.fromisoformat(time_str.replace("Z", "+00:00"))
            secs = (datetime.now(timezone.utc) - dt).total_seconds()
            if secs < 60:        time_label = "<1m"
            elif secs < 3600:    time_label = f"{int(secs//60)}m"
            elif secs < 86400:   time_label = f"{int(secs//3600)}h"
            else:                time_label = f"{int(secs//86400)}d"
        except Exception:
            time_label = "?"

    icon_img = svg_to_pil(svg_file, 22, icon_color)

    try:    font_big = ImageFont.truetype(FONT_BOLD, 24)
    except: font_big = ImageFont.load_default()
    try:    font_sm  = ImageFont.truetype(FONT_REG, 10)
    except: font_sm  = ImageFont.load_default()

    SIZE = 72
    img  = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))
    draw = ImageDraw.Draw(img)

    if icon_img:
        iw, ih = icon_img.size
        img.paste(icon_img, ((SIZE - iw) // 2, 4), icon_img)

    bbox = draw.textbbox((0, 0), time_label, font=font_big)
    tw   = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 28), time_label, font=font_big, fill=time_color)

    bbox2 = draw.textbbox((0, 0), label, font=font_sm)
    lw    = bbox2[2] - bbox2[0]
    draw.text(((SIZE - lw) // 2, SIZE - 13), label, font=font_sm,
              fill=(160, 160, 180, 255))

    img.save(out)
    print(out)


# ── toggle-switch / toggle-light ──────────────────────────────────────────────

def cmd_toggle(domain, entity):
    try:
        ha_post(f"/api/services/{domain}/toggle", {"entity_id": entity})
    except Exception:
        pass

# ── poll-doors ────────────────────────────────────────────────────────────────

DOORS = [
    ("3", "binary_sensor.haustur_contact",                    "🚪 HAUSTÜR"),
    ("4", "binary_sensor.terrassentur_contact",               "🚪 TERRASSE"),
    ("5", "binary_sensor.kuchentur_contact",                  "🚪 KÜCHENTÜR"),
    ("6", "binary_sensor.fahrrad_box_contact",               "🚪 FAHRRAD"),
    ("10", "binary_sensor.schlafzimmer_fenster_contact",      "🪟 SCHLAFZ.FENSTER"),
    ("klingel", "binary_sensor.0x00158d008c7c34b5_vibration", "🔔 KLINGEL"),
]
HA_BUTTON_KEY = 7
POLL_INTERVAL = 5

def push_deck_alert(label, duration="20s", bg="#cc2200"):
    try:
        data = json.dumps({"label": label, "color": "#ffffff",
                           "background": bg, "fontsize": 12,
                           "duration": duration}).encode()
        req = urllib.request.Request(
            f"{DECK_API}/key/{HA_BUTTON_KEY}", data=data,
            headers={"Content-Type": "application/json"},
        )
        urllib.request.urlopen(req, timeout=2)
    except Exception:
        pass

def cmd_icon_camera(entity, cache_name, label):
    from PIL import Image, ImageDraw, ImageFont
    import io

    out = CACHE / f"camera-{cache_name}.png"

    SIZE = 72
    try:
        req = urllib.request.Request(
            f"{HA_URL}/api/camera_proxy/{entity}",
            headers={"Authorization": f"Bearer {HA_TOKEN}"},
        )
        with urllib.request.urlopen(req, timeout=5) as r:
            snapshot = Image.open(io.BytesIO(r.read())).convert("RGB")
        # crop centre square and resize
        w, h = snapshot.size
        crop = min(w, h)
        left = (w - crop) // 2
        top  = (h - crop) // 2
        snapshot = snapshot.crop((left, top, left + crop, top + crop))
        snapshot = snapshot.resize((SIZE, SIZE), Image.LANCZOS)
        img = snapshot.convert("RGBA")
    except Exception:
        img = Image.new("RGBA", (SIZE, SIZE), (18, 18, 28, 255))

    # label bar at bottom
    overlay = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)
    draw.rectangle([(0, SIZE - 18), (SIZE, SIZE)], fill=(0, 0, 0, 160))
    try:    font = ImageFont.truetype(FONT_REG, 10)
    except: font = ImageFont.load_default()
    draw.text((SIZE // 2, SIZE - 9), label, font=font, fill=(220, 220, 220, 255), anchor="mm")
    img = Image.alpha_composite(img, overlay)

    img.save(out)
    print(out)


def cmd_icon_mower_badge(entity):
    """Render the HA icon; overlay a red lawnmower badge when <entity> reports an error.

    Healthy states: no_error / unavailable / unknown / ? / "" / rain_delay → plain ha.png.
    (rain_delay is informational — mower postponed due to rain — not a fault.)
    Any other value → error → red circular badge with a mower glyph (persists until cleared).
    """
    from PIL import Image, ImageDraw

    base_src = ASSETS / "ha.png"
    out = CACHE / "ha-mower.png"

    state = (get_state(entity) or "").strip().lower()
    # rain_delay is informational (mower postponed due to rain), not a fault → no badge.
    healthy = state in {"no_error", "unavailable", "unknown", "", "?", "rain_delay"}

    try:
        img = Image.open(base_src).convert("RGBA")
    except Exception:
        print(base_src)
        return

    if healthy:
        # No error → emit the plain icon (no badge).
        try:
            img.save(out)
            print(out)
        except Exception:
            print(base_src)
        return

    # Error → draw a red badge in the bottom-right corner with a mower glyph.
    W, H = img.size
    r = max(12, W // 4)              # badge radius scales with icon size
    cx, cy = W - r - 1, H - r - 1    # bottom-right
    draw = ImageDraw.Draw(img)
    # white outline ring for contrast, then solid red fill
    draw.ellipse([cx - r - 2, cy - r - 2, cx + r + 2, cy + r + 2], fill=(255, 255, 255, 255))
    draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(220, 38, 38, 255))

    # Mower glyph (MDI robot-mower, inline) tinted white, centered in the badge.
    g = _render_mower_glyph(int(r * 1.7))
    if g is not None:
        gx = cx - g.width // 2
        gy = cy - g.height // 2
        img.alpha_composite(g, (gx, gy))
    else:
        # Fallback: white exclamation mark if rendering failed.
        draw.text((cx, cy), "!", fill=(255, 255, 255, 255), anchor="mm")

    try:
        img.save(out)
        print(out)
    except Exception:
        print(base_src)


# Inline MDI robot-mower path (avoids network fetch which iconify now 403s).
_MOWER_PATH = (
    "M1 14C1 16.76 3.24 19 6 19C7.64 19 9.09 18.21 10 17H15.17C15.58 18.17 16.7 19 18 "
    "19C19.31 19 20.42 18.17 20.83 17H23V15C23 9.5 18.5 5 13 5H1V14M21 15H10.9C10.97 "
    "14.68 11 14.34 11 14C11 11.24 8.76 9 6 9C4.87 9 3.84 9.37 3 10V7H12.5C15.1 7 17.42 "
    "8.16 19 10H15V12H20.25C20.67 12.92 20.92 13.94 21 15M6 11C7.66 11 9 12.34 9 14C9 "
    "15.66 7.66 17 6 17C4.34 17 3 15.66 3 14C3 12.34 4.34 11 6 11Z"
)


def _render_mower_glyph(size: int, color: str = "#ffffff"):
    """Render the inline lawnmower SVG to a PIL RGBA image at the given size."""
    try:
        from PIL import Image
        svg = (
            f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" '
            f'viewBox="0 0 24 24"><path fill="{color}" d="{_MOWER_PATH}"/></svg>'
        )
        with tempfile.NamedTemporaryFile(suffix=".svg", mode="w", delete=False) as f:
            f.write(svg)
            tmp_svg = f.name
        tmp_png = tmp_svg.replace(".svg", ".png")
        subprocess.run(
            ["convert", "-background", "none",
             "-resize", f"{size}x{size}", f"svg:{tmp_svg}", tmp_png],
            capture_output=True,
        )
        img = Image.open(tmp_png).convert("RGBA")
        os.unlink(tmp_svg)
        os.unlink(tmp_png)
        return img
    except Exception:
        return None


def cmd_poll_doors():
    prev = {}
    while True:
        for idx, entity, label in DOORS:
            state = get_state(entity)
            if prev.get(idx) == "off" and state == "on":
                push_deck_alert(label)
            prev[idx] = state
        time.sleep(POLL_INTERVAL)

# ── main ──────────────────────────────────────────────────────────────────────

def main():
    cmd  = sys.argv[1] if len(sys.argv) > 1 else ""
    args = sys.argv[2:]

    if cmd == "icon-light" and len(args) >= 2:
        cmd_icon_light(args[0], args[1])
    elif cmd == "icon-door" and len(args) >= 3:
        cmd_icon_door(args[0], args[1], " ".join(args[2:]))
    elif cmd == "icon-moisture" and len(args) >= 3:
        cmd_icon_moisture(args[0], args[1], " ".join(args[2:]))
    elif cmd == "icon-bell" and len(args) >= 3:
        cmd_icon_bell(args[0], args[1], " ".join(args[2:]))
    elif cmd == "toggle-switch" and len(args) >= 1:
        cmd_toggle("switch", args[0])
    elif cmd == "icon-camera" and len(args) >= 3:
        cmd_icon_camera(args[0], args[1], " ".join(args[2:]))
    elif cmd == "toggle-light" and len(args) >= 1:
        cmd_toggle("light", args[0])
    elif cmd == "icon-mower-badge" and len(args) >= 1:
        cmd_icon_mower_badge(args[0])
    elif cmd == "poll-doors":
        cmd_poll_doors()
    else:
        print(__doc__, file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
