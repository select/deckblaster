#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Generate docs/screenshots/github.png with fake PR data.

Showcases every feature:
  • CI success / failure / pending progress bar / overrun bar
  • All review states (APR / WAIT / REQ / DRFT)
  • Comment count with mdi:message-outline icon
  • Average CI time in footer when pending
  • Header card with official octocat + PR count

Usage (from repo root):
    uv run deckmaster/decks/github/make-screenshot.py
"""
import importlib.util, json, sys, time, shutil
from datetime import datetime, timezone, timedelta
from pathlib import Path
from PIL import Image

SCRIPT_DIR = Path(__file__).parent
DECKMASTER  = SCRIPT_DIR.parents[1]   # …/deckmaster
REPO        = SCRIPT_DIR.parents[2]   # …/deckblaster
OUT_DIR     = REPO / "docs" / "screenshots"
TEMPLATE    = DECKMASTER / "decks" / "recorder" / "assets" / "steamdeck-template.png"
ASSETS      = DECKMASTER / "decks" / "assets"

# ── Mock data ─────────────────────────────────────────────────────────────────
now = datetime.now(timezone.utc)

CI_HISTORY = {
    "org/platform": {"ts": time.time(), "avg": 480.0},   # 8 min average
    "org/backend":  {"ts": time.time(), "avg": 240.0},   # 4 min average
}

MOCK_PRS = [
    # 1 — success, approved
    dict(number=42,  title="feat: add dark mode support",
         isDraft=False, url="#", repo="org/frontend",
         reviewDecision="approved",     comments=0,
         ciState="success",  ciStartedAt=None),
    # 2 — success, waiting, 3 comments
    dict(number=137, title="fix: memory leak in request handler",
         isDraft=False, url="#", repo="org/backend",
         reviewDecision="waiting",      comments=3,
         ciState="success",  ciStartedAt=None),
    # 3 — pending, 70 % progress (yellow bar), avg 8 min shown in footer
    dict(number=256, title="chore: upgrade all dependencies",
         isDraft=False, url="#", repo="org/platform",
         reviewDecision="none",         comments=0,
         ciState="pending",
         ciStartedAt=(now - timedelta(seconds=336)).strftime("%Y-%m-%dT%H:%M:%SZ")),
    # 4 — pending, 125 % overrun (orange full bar), approved
    dict(number=189, title="feat(api): pagination endpoint",
         isDraft=False, url="#", repo="org/platform",
         reviewDecision="approved",     comments=0,
         ciState="pending",
         ciStartedAt=(now - timedelta(seconds=600)).strftime("%Y-%m-%dT%H:%M:%SZ")),
    # 5 — success, changes requested, 2 comments
    dict(number=88,  title="docs: improve onboarding guide",
         isDraft=False, url="#", repo="org/docs",
         reviewDecision="changes_requested", comments=2,
         ciState="success",  ciStartedAt=None),
    # 6 — success, approved, 0 comments
    dict(number=412, title="refactor: split auth module",
         isDraft=False, url="#", repo="org/frontend",
         reviewDecision="approved",     comments=0,
         ciState="success",  ciStartedAt=None),
    # 7 — success, waiting, 8 comments
    dict(number=55,  title="feat: websocket pub/sub",
         isDraft=False, url="#", repo="org/backend",
         reviewDecision="waiting",      comments=8,
         ciState="success",  ciStartedAt=None),
    # 8 — failure, no review, no comments
    dict(number=623, title="fix: k8s resource limits",
         isDraft=False, url="#", repo="org/infra",
         reviewDecision="none",         comments=0,
         ciState="failure",  ciStartedAt=None),
    # 9 — draft, success
    dict(number=71,  title="test: add e2e coverage",
         isDraft=True,  url="#", repo="org/frontend",
         reviewDecision="none",         comments=0,
         ciState="success",  ciStartedAt=None),
    # 10 — pending, 40 % progress (yellow bar), 4 min avg
    dict(number=98,  title="fix: race condition in cache layer",
         isDraft=False, url="#", repo="org/backend",
         reviewDecision="none",         comments=0,
         ciState="pending",
         ciStartedAt=(now - timedelta(seconds=96)).strftime("%Y-%m-%dT%H:%M:%SZ")),
]

# ── Inject mock caches (TTL keeps get_prs() from calling GitHub) ──────────────
TMP = Path("/tmp/streamdeck-github")
TMP.mkdir(exist_ok=True)

Path("/tmp/streamdeck-github-prs.json").write_text(json.dumps({
    "ts":             time.time(),
    "prs":            MOCK_PRS,
    "has_pending_ci": True,
}))
Path("/tmp/streamdeck-github-ci-history.json").write_text(json.dumps(CI_HISTORY))

# Clear stale rendered images so the module re-renders from mock data
for p in list(TMP.glob("pr-*.png")) + [TMP / "header.png", TMP / "badge.png"]:
    p.unlink(missing_ok=True)

# ── Import renderer ───────────────────────────────────────────────────────────
spec = importlib.util.spec_from_file_location("gpr", SCRIPT_DIR / "github-prs.py")
mod  = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

prs = mod.get_prs()
assert prs[0]["number"] == 42, f"Cache not fresh — got real data: {prs[0]}"
print(f"Rendering {len(prs)} mock PRs…")
mod._render_all(prs)   # force render even when cache is fresh

# ── Collect the 15 key images ─────────────────────────────────────────────────
keys: list[Image.Image] = []

# Key 0 — header
keys.append(Image.open(TMP / "header.png").convert("RGBA"))

# Keys 1-12 — PR slots (mod._render_all rendered them in get_prs)
for n in range(12):
    keys.append(Image.open(TMP / f"pr-{n}.png").convert("RGBA"))

# Key 13 — empty (fully opaque dark card so paste is clean)
keys.append(Image.new("RGBA", (72, 72), (13, 17, 23, 255)))

# Key 14 — back: compose arrow onto a dark card so transparency doesn’t bleed
_back_src = Image.open(ASSETS / "back.png").convert("RGBA")
_back_card = Image.new("RGBA", (72, 72), (13, 17, 23, 255))
_bx = (72 - _back_src.width)  // 2
_by = (72 - _back_src.height) // 2
_back_card.paste(_back_src, (_bx, _by), _back_src)
keys.append(_back_card)

assert len(keys) == 15, f"Expected 15 keys, got {len(keys)}"

# ── Composite onto Stream Deck template (same as make-screenshots.py) ─────────
KEY_DST    = 84
V_GAP      = 9
X0, Y0     = 358, 309
GRID_RIGHT = 812
COLS, ROWS = 5, 3
CROP_BOX   = (298, 226, 870, 634)

col_xs = [X0 + int(col * (GRID_RIGHT - X0 - KEY_DST) / 4) for col in range(COLS)]

tmpl = Image.open(TEMPLATE).convert("RGBA")
tmpl = tmpl.resize((tmpl.width * 2, tmpl.height * 2), Image.LANCZOS)
comp = tmpl.copy()

for row in range(ROWS):
    for col in range(COLS):
        key = keys[row * COLS + col]
        # Supersample: 72 → 144 NEAREST (preserve pixel edges) → 84 LANCZOS (anti-alias down)
        key_up   = key.resize((144, 144), Image.NEAREST)
        key_final = key_up.resize((KEY_DST, KEY_DST), Image.LANCZOS)
        comp.paste(key_final, (col_xs[col], Y0 + row * (KEY_DST + V_GAP)))

OUT_DIR.mkdir(parents=True, exist_ok=True)
out = OUT_DIR / "github.png"
comp.crop(CROP_BOX).save(out)
print(f"Saved → {out}")
