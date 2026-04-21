#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow", "requests"]
# ///
"""Generate README screenshots for every plugin deck.

Cycles through each .deck file by temporarily running deckmaster
with that deck, capturing /screenshot from the HTTP API, and
compositing onto the Stream Deck photo template used by the recorder.

Output: docs/screenshots/<name>.png  (1170×852, 2× template scale)

Usage (from repo root):
    systemctl --user stop streamdeck.service
    uv run scripts/make-screenshots.py
    systemctl --user start streamdeck.service

Or let the script manage start/stop automatically (default).
"""
import os
import sys
import time
import signal
import subprocess
import urllib.request
from pathlib import Path
from PIL import Image

REPO       = Path(__file__).parent.parent
DECKMASTER = REPO / "deckmaster"
DECKS      = DECKMASTER / "decks"
BINARY     = DECKMASTER / "deckmaster"
OUT_DIR    = REPO / "docs" / "screenshots"
API_PORT   = 9990
API_URL    = f"http://localhost:{API_PORT}"

TEMPLATE   = DECKS / "recorder" / "assets" / "steamdeck-template.png"

# Recorder compositing constants (2× upscale of 585×426 template)
KEY_SRC    = 72
GAP_SRC    = 4
KEY_DST    = 84
V_GAP      = 9
X0, Y0     = 358, 309
GRID_RIGHT = 812
COLS, ROWS = 5, 3
col_xs     = [X0 + int(col * (GRID_RIGHT - X0 - KEY_DST) / 4) for col in range(COLS)]

# Decks to screenshot: (output_name, deck_path_relative_to_DECKMASTER, wait_seconds)
DECKS_TO_SHOOT = [
    ("main",            "decks/main.deck",                     5),
    ("ha",              "decks/ha/ha.deck",                    6),
    ("zoom",            "decks/zoom/zoom.deck",                4),
    ("slots",           "decks/slots/slots.deck",              6),
    ("calc",            "decks/calc/calc.deck",                5),
    ("highlight",       "decks/highlight/highlight.deck",      4),
    ("polymarket",      "decks/polymarket/polymarket.deck",    6),
    ("polymarket-cats", "decks/polymarket/polymarket-cats.deck", 4),
    ("docker",          "decks/docker/docker.deck",            5),
    ("ports",           "decks/ports/ports.deck",              5),
    ("apps",            "decks/apps.deck",                     4),
]

# ── helpers ───────────────────────────────────────────────────────────────────

def wait_for_api(timeout=10):
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            urllib.request.urlopen(f"{API_URL}/health", timeout=1)
            return True
        except Exception:
            time.sleep(0.2)
    return False

def screenshot() -> Image.Image:
    with urllib.request.urlopen(f"{API_URL}/screenshot", timeout=5) as r:
        data = r.read()
    import io
    return Image.open(io.BytesIO(data)).convert("RGBA")

def composite(shot: Image.Image) -> Image.Image:
    tmpl = Image.open(TEMPLATE).convert("RGBA")
    tmpl = tmpl.resize((tmpl.width * 2, tmpl.height * 2), Image.LANCZOS)
    comp = tmpl.copy()

    for row in range(ROWS):
        for col in range(COLS):
            sx = col * (KEY_SRC + GAP_SRC)
            sy = row * (KEY_SRC + GAP_SRC)
            key = shot.crop((sx, sy, sx + KEY_SRC, sy + KEY_SRC))
            key = key.resize((KEY_DST, KEY_DST), Image.LANCZOS)
            # Round the corners a little
            comp.paste(key, (col_xs[col], Y0 + row * (KEY_DST + V_GAP)))

    return comp

def stop_service():
    subprocess.run(["systemctl", "--user", "stop", "streamdeck.service"],
                   capture_output=True)
    time.sleep(1)

def start_service():
    subprocess.run(["systemctl", "--user", "start", "streamdeck.service"],
                   capture_output=True)

# ── main ──────────────────────────────────────────────────────────────────────

def main():
    if not BINARY.exists():
        print("ERROR: deckmaster binary not found — run: cd deckmaster && go build ./...")
        sys.exit(1)

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    # Load env so HA_TOKEN etc. are available for widget rendering
    env_file = Path.home() / ".config" / "streamdeck.env"
    env = dict(os.environ)
    env["PATH"] = f"{Path.home()}/.bun/bin:{Path.home()}/.local/bin:{env.get('PATH', '')}"
    if env_file.exists():
        for line in env_file.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                k, _, v = line.partition("=")
                env[k.strip()] = v.strip()

    print("Stopping streamdeck service…")
    stop_service()
    time.sleep(1)

    try:
        for name, deck_rel, wait_secs in DECKS_TO_SHOOT:
            deck_path = DECKMASTER / deck_rel
            if not deck_path.exists():
                print(f"  SKIP  {name} (deck not found: {deck_path})")
                continue

            print(f"  📸  {name}…", end="", flush=True)

            proc = subprocess.Popen(
                [str(BINARY),
                 "-deck", str(deck_path),
                 "-api", f":{API_PORT}",
                 "-brightness", "60"],
                cwd=str(DECKMASTER),
                env=env,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )

            if not wait_for_api():
                print(f" API not ready, skipping")
                proc.terminate()
                proc.wait()
                continue

            time.sleep(wait_secs)  # let widgets render

            try:
                shot = screenshot()
                img  = composite(shot)
                out  = OUT_DIR / f"{name}.png"
                img.save(out)
                print(f" saved → docs/screenshots/{name}.png")
            except Exception as e:
                print(f" ERROR: {e}")

            proc.terminate()
            proc.wait()
            time.sleep(0.5)

    finally:
        print("\nRestarting streamdeck service…")
        start_service()
        print("Done.")

if __name__ == "__main__":
    main()
