#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""
Generate docs/screenshots/jira.png with fake issue data.

Showcases every feature:
  • Status colors: To Do (blue), Blocked (red), In Progress (yellow), ON REVIEW (purple)
  • Priority dots (High=red, Medium=yellow, Low=green)
  • Issue types (Bug, Task)
  • Board labels (EN, AW)
  • Badge with corner counts (open, blocked, in progress, total)
  • Pagination button (page 1/2)
  • Header with issue count + status summary

Usage (from repo root):
    uv run deckmaster/decks/jira/make-screenshot.py
"""
import json, sys, time, shutil
from pathlib import Path
from PIL import Image

SCRIPT_DIR = Path(__file__).parent
DECKMASTER = SCRIPT_DIR.parents[1]   # …/deckmaster
REPO       = SCRIPT_DIR.parents[2]   # …/deckblaster
OUT_DIR    = REPO / "docs" / "screenshots"
TEMPLATE   = DECKMASTER / "decks" / "recorder" / "assets" / "steamdeck-template.png"
ASSETS     = DECKMASTER / "decks" / "assets"

# ── Mock data ─────────────────────────────────────────────────────────────────

MOCK_ISSUES = [
    # To Do issues (blue header)
    dict(key="FE-142", title="Add dark mode toggle to settings",
         status="To Do", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-28T10:00:00Z"),
    dict(key="BE-87", title="Fix rate limiter bypass on API",
         status="To Do", priority="High", type="Bug",
         assignee="Dev User", url="#", board="BE", updated="2026-05-27T14:00:00Z"),
    dict(key="FE-143", title="Create onboarding wizard component",
         status="To Do", priority="Low", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-27T09:00:00Z"),
    dict(key="BE-88", title="Improve search query performance",
         status="To Do", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="BE", updated="2026-05-26T16:00:00Z"),
    dict(key="FE-144", title="Integrate analytics dashboard",
         status="To Do", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-26T11:00:00Z"),
    # Blocked issues (red header)
    dict(key="BE-89", title="Migrate auth service to OAuth2",
         status="Blocked", priority="High", type="Task",
         assignee="Dev User", url="#", board="BE", updated="2026-05-28T08:00:00Z"),
    dict(key="FE-145", title="Deploy blocked by CI flakiness",
         status="Blocked", priority="Highest", type="Bug",
         assignee="Dev User", url="#", board="FE", updated="2026-05-27T12:00:00Z"),
    # In Progress (yellow header)
    dict(key="FE-146", title="Responsive layout for mobile",
         status="In Progress", priority="High", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-28T09:30:00Z"),
    dict(key="BE-90", title="Add webhook retry mechanism",
         status="In Progress", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="BE", updated="2026-05-27T15:00:00Z"),
    # ON REVIEW (purple header)
    dict(key="FE-147", title="Refactor form validation logic",
         status="ON REVIEW", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-28T07:00:00Z"),
    dict(key="BE-91", title="Fix race condition in cache",
         status="ON REVIEW", priority="High", type="Bug",
         assignee="Dev User", url="#", board="BE", updated="2026-05-27T18:00:00Z"),
    dict(key="BE-92", title="Add pagination to list endpoint",
         status="ON REVIEW", priority="Low", type="Task",
         assignee="Dev User", url="#", board="BE", updated="2026-05-27T10:00:00Z"),
    # Extra issues for page 2
    dict(key="FE-148", title="Add E2E test coverage",
         status="To Do", priority="Medium", type="Task",
         assignee="Dev User", url="#", board="FE", updated="2026-05-25T14:00:00Z"),
    dict(key="BE-93", title="Implement full-text search",
         status="To Do", priority="Low", type="Task",
         assignee="Dev User", url="#", board="BE", updated="2026-05-25T10:00:00Z"),
]

# ── Inject mock cache ─────────────────────────────────────────────────────────
TMP = Path("/tmp/streamdeck-jira")
TMP.mkdir(exist_ok=True)

Path("/tmp/streamdeck-jira-issues.json").write_text(json.dumps({
    "ts":     time.time(),
    "issues": MOCK_ISSUES,
}))

# Set page to 0 for screenshot
Path("/tmp/streamdeck-jira-page.json").write_text(json.dumps({"page": 0}))

# Clear stale rendered images so the script re-renders from mock data
for p in TMP.glob("*.png"):
    p.unlink(missing_ok=True)

# ── Render via TS ─────────────────────────────────────────────────────────────
import subprocess

# icon 0 triggers getIssues() → renderAll() which writes all slots + header + badge + page
result = subprocess.run(
    ["bun", str(SCRIPT_DIR / "jira-issues.ts"), "icon", "0"],
    cwd=str(DECKMASTER), capture_output=True, text=True,
)
if result.returncode != 0:
    print("TS render failed:", result.stderr, file=sys.stderr)
    sys.exit(1)

# Also render header and page-icon explicitly
subprocess.run(
    ["bun", str(SCRIPT_DIR / "jira-issues.ts"), "header"],
    cwd=str(DECKMASTER), capture_output=True,
)
subprocess.run(
    ["bun", str(SCRIPT_DIR / "jira-issues.ts"), "page-icon"],
    cwd=str(DECKMASTER), capture_output=True,
)

print(f"Rendering {len(MOCK_ISSUES)} mock issues…")

# ── Collect the 15 key images ────────────────────────────────────────────────
keys: list[Image.Image] = []

# Key 0 — header
keys.append(Image.open(TMP / "header.png").convert("RGBA"))

# Keys 1-12 — issue slots
for n in range(12):
    p = TMP / f"issue-{n}.png"
    if p.exists():
        keys.append(Image.open(p).convert("RGBA"))
    else:
        keys.append(Image.new("RGBA", (72, 72), (13, 17, 23, 255)))

# Key 13 — pagination
keys.append(Image.open(TMP / "page.png").convert("RGBA"))

# Key 14 — back arrow
_back_src = Image.open(ASSETS / "back.png").convert("RGBA")
_back_card = Image.new("RGBA", (72, 72), (13, 17, 23, 255))
_bx = (72 - _back_src.width)  // 2
_by = (72 - _back_src.height) // 2
_back_card.paste(_back_src, (_bx, _by), _back_src)
keys.append(_back_card)

assert len(keys) == 15, f"Expected 15 keys, got {len(keys)}"

# ── Composite onto Stream Deck template (4× for crisp output) ────────────────
SCALE      = 4
KEY_DST    = 84  * SCALE // 2   # 168
V_GAP      = 9   * SCALE // 2   # 18
X0         = 358 * SCALE // 2   # 716
Y0         = 309 * SCALE // 2   # 618
GRID_RIGHT = 812 * SCALE // 2   # 1624
COLS, ROWS = 5, 3
CROP_BOX   = tuple(c * SCALE // 2 for c in (298, 226, 870, 634))

col_xs = [X0 + int(col * (GRID_RIGHT - X0 - KEY_DST) / 4) for col in range(COLS)]

tmpl = Image.open(TEMPLATE).convert("RGBA")
tmpl = tmpl.resize((tmpl.width * SCALE, tmpl.height * SCALE), Image.LANCZOS)
comp = tmpl.copy()

for row in range(ROWS):
    for col in range(COLS):
        key = keys[row * COLS + col]
        key_final = key.resize((KEY_DST, KEY_DST), Image.LANCZOS)
        comp.paste(key_final, (col_xs[col], Y0 + row * (KEY_DST + V_GAP)))

OUT_DIR.mkdir(parents=True, exist_ok=True)
out = OUT_DIR / "jira.png"
comp.crop(CROP_BOX).save(out)
print(f"Saved → {out}")

# Also save badge at 2× for README
badge_src = Image.open(TMP / "badge.png").convert("RGBA")
badge_src = badge_src.resize((144, 144), Image.LANCZOS)
badge_out = OUT_DIR / "jira-badge.png"
badge_src.save(badge_out)
print(f"Saved → {badge_out}")
