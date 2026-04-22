#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Generate docs/screenshots/calendar.png with mock calendar events.

Showcases every feature:
  • All 5 urgency color tiers (now, ≤20min, ≤1h, ≤4h, >4h) + past (grey)
  • Header with day label, date, event count
  • Navigation buttons (prev/next/exit)
  • 9 event slots in 3×3 column-major grid
  • Text wrapping for long titles

Usage (from repo root):
    uv run deckmaster/decks/calendar/make-screenshot.py
"""
import json
import subprocess
import time
from datetime import datetime, timezone, timedelta
from pathlib import Path
from PIL import Image

SCRIPT_DIR  = Path(__file__).parent
DECKMASTER  = SCRIPT_DIR.parents[1]
REPO        = SCRIPT_DIR.parents[2]
OUT_DIR     = REPO / "docs" / "screenshots"
TEMPLATE    = DECKMASTER / "decks" / "recorder" / "assets" / "steamdeck-template.png"
ASSETS      = DECKMASTER / "decks" / "assets"
CAL_ASSETS  = SCRIPT_DIR / "assets"

CACHE_DIR   = Path.home() / ".local" / "share" / "deckblaster"
STATE_FILE  = CACHE_DIR / "calendar-day.json"
EVENTS_FILE = CACHE_DIR / "calendar-day-events.json"
IMG_DIR     = CACHE_DIR / "calendar-day"

# ── Mock events for "today" showing all color tiers ──────────────────────────

now = datetime.now().astimezone()

def evt(summary, start_offset_min, duration_min=30, location=""):
    start = now + timedelta(minutes=start_offset_min)
    end = start + timedelta(minutes=duration_min)
    return {
        "dt": start.isoformat(),
        "end_dt": end.isoformat(),
        "summary": summary,
        "location": location,
    }

MOCK_EVENTS = [
    # past (grey) — ended 30min ago
    evt("Morning standup",       -60, 30),
    # now (red) — currently running
    evt("Sprint planning",       -10, 60),
    # ≤20min (orange) — starts in 12 min
    evt("Design review",          12, 45),
    # ≤1h (yellow) — starts in 40 min
    evt("Architecture forum",     40, 60),
    # ≤4h (blue) — starts in 2.5h
    evt("Team retrospective",    150, 60, "https://meet.example.com/retro"),
    # >4h (green) — starts in 5h
    evt("Quarterly roadmap review", 300, 90),
    # >4h (green) — starts in 6h
    evt("1:1 weekly sync",       360, 30),
    # >4h (green) — starts in 7h
    evt("End of day wrap-up",    420, 15),
    # >4h (green) — starts in 8h
    evt("Optional social hour",  480, 60),
]

# ── Write mock data and render via bun ────────────────────────────────────────

CACHE_DIR.mkdir(parents=True, exist_ok=True)

# Back up existing files
state_backup = STATE_FILE.read_text() if STATE_FILE.exists() else None
events_backup = EVENTS_FILE.read_text() if EVENTS_FILE.exists() else None

try:
    # Write mock events
    EVENTS_FILE.write_text(json.dumps({
        "days": {"0": MOCK_EVENTS},
        "fetchedAt": int(time.time() * 1000),
    }))

    # Write state (today)
    STATE_FILE.write_text(json.dumps({"dayOffset": 0}))

    # Render via bun (init re-renders all images)
    subprocess.run(
        ["bun", str(SCRIPT_DIR / "calendar-day.js"), "init"],
        cwd=str(DECKMASTER),
        check=True,
        capture_output=True,
    )

    # ── Collect 15 key images ─────────────────────────────────────────────────
    # Layout:
    #   [HEAD] [s0 ] [s3 ] [s6 ] [    ]
    #   [PREV] [s1 ] [s4 ] [s7 ] [NEXT]
    #   [EXIT] [s2 ] [s5 ] [s8 ] [    ]
    #
    # Key indices: 0=head, 1=s0, 2=s3, 3=s6, 4=empty,
    #              5=prev, 6=s1, 7=s4, 8=s7, 9=next,
    #              10=exit, 11=s2, 12=s5, 13=s8, 14=empty

    keys: list[Image.Image] = []

    # Row 0: header, s0, s3, s6, empty
    keys.append(Image.open(IMG_DIR / "header.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-0.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-3.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-6.png").convert("RGBA"))
    keys.append(Image.new("RGBA", (72, 72), (0, 0, 0, 255)))

    # Row 1: prev, s1, s4, s7, next
    keys.append(Image.open(CAL_ASSETS / "prev.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-1.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-4.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-7.png").convert("RGBA"))
    keys.append(Image.open(CAL_ASSETS / "next.png").convert("RGBA"))

    # Row 2: exit, s2, s5, s8, empty
    keys.append(Image.open(CAL_ASSETS / "exit.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-2.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-5.png").convert("RGBA"))
    keys.append(Image.open(IMG_DIR / "key-8.png").convert("RGBA"))
    keys.append(Image.new("RGBA", (72, 72), (0, 0, 0, 255)))

    assert len(keys) == 15

    # ── Composite onto Stream Deck template (4× for crisp output) ─────────────
    SCALE      = 4
    KEY_DST    = 84  * SCALE // 2
    V_GAP      = 9   * SCALE // 2
    X0         = 358 * SCALE // 2
    Y0         = 309 * SCALE // 2
    GRID_RIGHT = 812 * SCALE // 2
    CROP_BOX   = tuple(c * SCALE // 2 for c in (298, 226, 870, 634))

    col_xs = [X0 + int(col * (GRID_RIGHT - X0 - KEY_DST) / 4) for col in range(5)]

    tmpl = Image.open(TEMPLATE).convert("RGBA")
    tmpl = tmpl.resize((tmpl.width * SCALE, tmpl.height * SCALE), Image.LANCZOS)
    comp = tmpl.copy()

    for row in range(3):
        for col in range(5):
            key = keys[row * 5 + col]
            key_final = key.resize((KEY_DST, KEY_DST), Image.LANCZOS)
            comp.paste(key_final, (col_xs[col], Y0 + row * (KEY_DST + V_GAP)))

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    out = OUT_DIR / "calendar.png"
    comp.crop(CROP_BOX).save(out)
    print(f"Saved → {out}")

    # ── Calendar widget (single key for main deck) ───────────────────────
    # Render a mock "IN 9H" event icon matching next-event.py's design
    W, H, PAD = 72, 72, 2
    FONT = "DejaVu Sans,sans-serif"
    widget_svg = f"""<svg width="{W}" height="{H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="{W}" height="{H}" fill="#000000"/>
  <rect x="5" y="5" width="62" height="14" rx="4" fill="#3fb950"/>
  <text x="36" y="15" font-family="{FONT}" font-weight="bold" font-size="9" fill="#000000" text-anchor="middle">IN 9H</text>
  <text x="7" y="30" font-family="{FONT}" font-size="9" fill="#8b949e">09:00 – 13:00</text>
  <text x="7" y="42" font-family="{FONT}" font-size="9" fill="#e6edf3">Focus</text>
  <text x="7" y="53" font-family="{FONT}" font-size="9" fill="#e6edf3">mornings:</text>
  <text x="7" y="64" font-family="{FONT}" font-size="9" fill="#e6edf3">meeting…</text>
</svg>"""
    widget_out = OUT_DIR / "calendar-widget.png"
    subprocess.run(
        ["bun", "-e",
         f"const sharp=require('sharp');sharp(Buffer.from({json.dumps(widget_svg)})).png()"
         f".toFile({json.dumps(str(widget_out))}).then(()=>process.exit(0))"],
        capture_output=True, cwd=str(DECKMASTER),
    )
    print(f"Saved → {widget_out}")

finally:
    # Restore original files
    if events_backup is not None:
        EVENTS_FILE.write_text(events_backup)
    elif EVENTS_FILE.exists():
        EVENTS_FILE.unlink()

    if state_backup is not None:
        STATE_FILE.write_text(state_backup)
    elif STATE_FILE.exists():
        STATE_FILE.unlink()
