#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["Pillow"]
# ///
"""GitHub open-PR plugin for Stream Deck.

Commands
--------
  github-prs.py badge        live badge for main.deck navigation button
  github-prs.py header       header icon for github.deck key 0
  github-prs.py icon <n>     PR card icon for slot n  (0-indexed, up to 11)

Smart-polling TTL
-----------------
  CI running (any PR)            → 30 s  (always, overrides snooze)
  No running CI + button pressed → 60 s  (for 1 h after last press)
  No running CI, not snoozed    → 30 min
"""

import sys, json, os, subprocess, time, textwrap, fcntl, tempfile, urllib.request
from datetime import datetime, timezone
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

# ── Paths ─────────────────────────────────────────────────────────────────────
CACHE_FILE      = Path("/tmp/streamdeck-github-prs.json")
LOCK_FILE       = Path("/tmp/streamdeck-github.lock")
SNOOZE_FILE     = Path("/tmp/streamdeck-github-snoozed")
CI_HISTORY_FILE = Path("/tmp/streamdeck-github-ci-history.json")
OUT             = Path("/tmp/streamdeck-github")
OUT.mkdir(exist_ok=True)
ASSETS          = Path(__file__).parent / "assets"

# ── Tuning ────────────────────────────────────────────────────────────────────
MAX_PR_SLOTS = 12          # keys 1-12 in github.deck
KEY_SIZE     = 72
TTL_FAST        = 30          # seconds – any PR has running CI
TTL_SLOW        = 1800        # seconds – 30 min when all CIs are resolved
TTL_SNOOZED     = 60          # seconds – 1 min after a PR button was pressed
SNOOZE_DURATION = 3600        # seconds – snooze lasts 1 h

# ── Fonts ─────────────────────────────────────────────────────────────────────
_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
_REG  = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"

def _fnt(path, size):
    try:   return ImageFont.truetype(path, size)
    except: return ImageFont.load_default()

F_TITLE  = _fnt(_REG,  8)
F_META   = _fnt(_REG,  7)
F_STATUS = _fnt(_BOLD, 7)
F_BIG    = _fnt(_BOLD, 18)
F_MED    = _fnt(_BOLD, 12)

# ── Colour palette (GitHub dark) ──────────────────────────────────────────────
C_BG     = (13,  17,  23)
C_CARD   = (22,  27,  34)
C_BORDER = (48,  54,  61)
C_WHITE  = (255, 255, 255)
C_GRAY   = (110, 118, 129)
C_GREEN  = (63,  185,  80)
C_RED    = (248,  81,  73)
C_ORANGE = (210, 120,  40)   # overrun: still running but past expected time
C_YELLOW = (210, 153,  34)
C_BLUE   = ( 88, 166, 255)

CI_COLOR = {"success": C_GREEN, "failure": C_RED, "pending": C_YELLOW, "none": C_GRAY}
CI_LABEL = {"success": "CI+",   "failure": "CI!",  "pending": "CI~",    "none": ""}

RV_COLOR = {
    "approved":          C_GREEN,
    "changes_requested": C_RED,
    "waiting":           C_YELLOW,
    "draft":             C_GRAY,
    "none":              C_GRAY,
}
RV_LABEL = {
    "approved":          "APR",
    "changes_requested": "REQ",
    "waiting":           "WAIT",
    "draft":             "DRFT",
    "none":              "",
}

# ── GitHub GraphQL query ──────────────────────────────────────────────────────
_QUERY = """{
  search(query: "is:pr is:open author:@me", type: ISSUE, first: 12) {
    nodes {
      ... on PullRequest {
        number
        title
        isDraft
        url
        repository { nameWithOwner isArchived }
        reviewDecision
        comments { totalCount }
        commits(last: 1) {
          nodes {
            commit {
              statusCheckRollup {
                state
                contexts(last: 30) {
                  nodes {
                    ... on CheckRun {
                      status
                      startedAt
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}"""

_CI_STATE_MAP = {
    "success":   "success",
    "failure":   "failure",
    "error":     "failure",
    "pending":   "pending",
    "expected":  "pending",
}
_RV_MAP = {
    "APPROVED":           "approved",
    "CHANGES_REQUESTED":  "changes_requested",
    "REVIEW_REQUIRED":    "waiting",
}

def _fetch_from_github():
    r = subprocess.run(
        ["gh", "api", "graphql", "-f", f"query={_QUERY}"],
        capture_output=True, text=True, timeout=15,
    )
    if r.returncode != 0:
        return None
    nodes = (json.loads(r.stdout)
             .get("data", {})
             .get("search", {})
             .get("nodes", []) or [])
    prs = []
    for n in nodes:
        if not n:
            continue
        if (n.get("repository") or {}).get("isArchived"):
            continue
        commits = (n.get("commits") or {}).get("nodes") or []
        ci = "none"
        ci_started_at = None
        if commits:
            rollup = (commits[0].get("commit") or {}).get("statusCheckRollup")
            if rollup:
                ci = _CI_STATE_MAP.get(rollup.get("state", "").lower(), "none")
                contexts = (rollup.get("contexts") or {}).get("nodes") or []
                running_starts = [
                    c["startedAt"] for c in contexts
                    if c.get("status") == "IN_PROGRESS" and c.get("startedAt")
                ]
                if running_starts:
                    ci_started_at = min(running_starts)
        prs.append({
            "number":         n.get("number"),
            "title":          n.get("title", ""),
            "isDraft":        n.get("isDraft", False),
            "url":            n.get("url", ""),
            "repo":           (n.get("repository") or {}).get("nameWithOwner", ""),
            "reviewDecision": _RV_MAP.get(n.get("reviewDecision") or "", "none"),
            "comments":       (n.get("comments") or {}).get("totalCount", 0),
            "ciState":        ci,
            "ciStartedAt":    ci_started_at,
        })
    return prs


CI_HISTORY_TTL = 86400  # 24 h — re-sample once per day


def _load_ci_history():
    try:
        return json.loads(CI_HISTORY_FILE.read_text())
    except Exception:
        return {}


def _fetch_repo_ci_avg(repo):
    """Sample CI wall-clock time from recent merged PRs via GraphQL.
    Wall-clock = max(completedAt) - min(startedAt) across all check runs
    on the head commit of each merged PR.
    """
    owner, name = repo.split("/", 1)
    query = """{
  repository(owner: "%s", name: "%s") {
    pullRequests(last: 20, states: MERGED, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes {
        commits(last: 1) {
          nodes {
            commit {
              statusCheckRollup {
                contexts(last: 30) {
                  nodes {
                    ... on CheckRun {
                      startedAt
                      completedAt
                      conclusion
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}""" % (owner, name)
    r = subprocess.run(
        ["gh", "api", "graphql", "-f", f"query={query}"],
        capture_output=True, text=True, timeout=15,
    )
    if r.returncode != 0:
        return None
    prs_data = (
        json.loads(r.stdout)
        .get("data", {})
        .get("repository", {})
        .get("pullRequests", {})
        .get("nodes", []) or []
    )
    durations = []
    for pr_node in prs_data:
        commits = (pr_node.get("commits") or {}).get("nodes") or []
        if not commits:
            continue
        rollup = (commits[0].get("commit") or {}).get("statusCheckRollup")
        if not rollup:
            continue
        contexts = (rollup.get("contexts") or {}).get("nodes") or []
        starts, ends = [], []
        for c in contexts:
            if not c or c.get("conclusion") not in ("SUCCESS", "SKIPPED", "NEUTRAL"):
                continue
            if c.get("startedAt"):
                starts.append(c["startedAt"])
            if c.get("completedAt"):
                ends.append(c["completedAt"])
        if not starts or not ends:
            continue
        try:
            t_start = datetime.fromisoformat(min(starts).replace("Z", "+00:00"))
            t_end   = datetime.fromisoformat(max(ends).replace("Z", "+00:00"))
            d = (t_end - t_start).total_seconds()
            if 10 < d < 7200:
                durations.append(d)
        except Exception:
            pass
    return sum(durations) / len(durations) if len(durations) >= 3 else None


def get_ci_avg(repo):
    """Return cached avg CI duration (seconds) for repo, refreshing if stale."""
    history = _load_ci_history()
    now     = time.time()
    entry   = history.get(repo, {})
    if entry and now - entry.get("ts", 0) < CI_HISTORY_TTL:
        return entry.get("avg")
    avg = _fetch_repo_ci_avg(repo)
    if avg is not None:
        history[repo] = {"ts": now, "avg": avg}
        try:
            CI_HISTORY_FILE.write_text(json.dumps(history))
        except Exception:
            pass
    return avg or entry.get("avg")  # fall back to stale data on network error


def _is_snoozed():
    """True while within SNOOZE_DURATION of the last PR button press."""
    try:
        return time.time() - float(SNOOZE_FILE.read_text()) < SNOOZE_DURATION
    except Exception:
        return False

def _snooze():
    SNOOZE_FILE.write_text(str(time.time()))


def get_prs():
    """Return PR list.  Fetches from GitHub when TTL expired; serialised by lock."""
    now = time.time()
    with open(LOCK_FILE, "w") as lf:
        fcntl.flock(lf, fcntl.LOCK_EX)

        # ── try cache ──────────────────────────────────────────────────────
        if CACHE_FILE.exists():
            try:
                cached = json.loads(CACHE_FILE.read_text())
                has_pending = cached.get("has_pending_ci", False)
                ttl = TTL_FAST if has_pending else (TTL_SNOOZED if _is_snoozed() else TTL_SLOW)
                if now - cached.get("ts", 0) < ttl:
                    return cached["prs"]
            except Exception:
                pass

        # ── fetch fresh data ───────────────────────────────────────────────
        prs = _fetch_from_github()
        if prs is None:
            # network error – return stale data if available
            try:
                return json.loads(CACHE_FILE.read_text())["prs"]
            except Exception:
                return []

        has_pending = any(p["ciState"] == "pending" for p in prs)
        CACHE_FILE.write_text(json.dumps({
            "ts":             now,
            "prs":            prs,
            "has_pending_ci": has_pending,
        }))

        # Pre-render every image so subsequent icon calls just return paths
        _render_all(prs)
        return prs


# ── Drawing helpers ───────────────────────────────────────────────────────────
def _rr(draw, x0, y0, x1, y1, r, fill):
    draw.rounded_rectangle([x0, y0, x1, y1], radius=r, fill=fill)


def _fetch_mdi(name, dest: Path):
    """Download an MDI SVG from iconify if not already cached (same as ha.py)."""
    if not dest.exists():
        try:
            urllib.request.urlretrieve(
                f"https://api.iconify.design/mdi/{name}.svg", dest
            )
        except Exception:
            pass


def _svg_to_pil(svg_path: Path, size: int, color: str):
    """Rasterise an MDI SVG to a PIL Image via ImageMagick (same as ha.py)."""
    try:
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


# Message-outline icon pre-loaded from assets (downloaded once, persists)
_MSG_ICON: "Image.Image | None" = (
    Image.open(ASSETS / "mdi-message-outline.png").convert("RGBA")
    if (ASSETS / "mdi-message-outline.png").exists() else None
)


# ── PR card ───────────────────────────────────────────────────────────────────
_OCTOCAT = Image.open(ASSETS / "octocat-white.png").convert("RGBA")

def _render_pr_icon(pr, idx):
    img  = Image.new("RGBA", (KEY_SIZE, KEY_SIZE), (*C_BG, 255))
    draw = ImageDraw.Draw(img)

    ci  = pr["ciState"]
    rv  = "draft" if pr["isDraft"] else pr["reviewDecision"]
    nc  = pr["comments"]

    ci_c = CI_COLOR[ci]
    rv_c = RV_COLOR[rv]
    rv_l = RV_LABEL[rv]

    # Card background
    _rr(draw, 1, 1, KEY_SIZE-2, KEY_SIZE-2, 5, (*C_CARD, 255))

    # Pre-compute CI avg once — used by both progress bar and footer
    ci_avg = get_ci_avg(pr["repo"]) if ci == "pending" else None

    # Top strip: progress bar when CI is running, static colour otherwise
    BAR_Y1 = 5
    if ci == "pending" and pr.get("ciStartedAt"):
        try:
            started = datetime.fromisoformat(pr["ciStartedAt"].replace("Z", "+00:00"))
            elapsed = (datetime.now(timezone.utc) - started).total_seconds()
            if ci_avg and ci_avg > 0:
                progress  = elapsed / ci_avg      # > 1.0 means overrun
                fill_c    = C_ORANGE if progress >= 1.0 else C_YELLOW
                fill_w    = max(2, min(int(min(progress, 1.0) * (KEY_SIZE - 2)), KEY_SIZE - 2))
                _rr(draw, 1,          1, KEY_SIZE-2, BAR_Y1, 2, (*C_BORDER, 255))  # track
                _rr(draw, 1,          1, 1 + fill_w, BAR_Y1, 2, (*fill_c,   255))  # fill
            else:
                _rr(draw, 1, 1, KEY_SIZE-2, BAR_Y1, 3, (*ci_c, 255))  # no history yet
        except Exception:
            _rr(draw, 1, 1, KEY_SIZE-2, BAR_Y1, 3, (*ci_c, 255))
    else:
        _rr(draw, 1, 1, KEY_SIZE-2, BAR_Y1, 3, (*ci_c, 255))

    # Repo name (top-left) + PR number (top-right)
    repo_short = pr["repo"].split("/")[-1][:11]
    draw.text((3,          8), repo_short,         font=F_META, fill=(*C_GRAY, 255), anchor="la")
    draw.text((KEY_SIZE-3, 8), f"#{pr['number']}", font=F_META, fill=(*C_GRAY, 255), anchor="ra")

    # Title (up to 3 lines × 10 px)
    lines = textwrap.wrap(pr["title"], width=13)[:3]
    y = 18
    for line in lines:
        draw.text((3, y), line, font=F_TITLE, fill=(*C_WHITE, 255))
        y += 10

    # Status bar (bottom 15 px)
    bar_y  = KEY_SIZE - 16
    bar_cy = bar_y + (KEY_SIZE - 2 - bar_y) // 2  # vertical centre of bar
    _rr(draw, 1, bar_y, KEY_SIZE-2, KEY_SIZE-2, 3, (*C_BORDER, 255))

    # CI label (left, vertically centred)
    ci_label = CI_LABEL[ci]
    if ci_label:
        draw.text((4, bar_cy), ci_label, font=F_STATUS, fill=(*ci_c, 255), anchor="lm")

    # Centre: avg CI time when running, comment count otherwise
    if ci == "pending":
        if ci_avg:
            mins = int(ci_avg / 60)
            ctr_text  = f"{mins}m" if mins < 60 else f"{mins//60}h{mins%60:02d}m"
            ctr_color = C_YELLOW
        else:
            ctr_text, ctr_color = None, C_GRAY
    elif nc > 0:
        if _MSG_ICON is not None:
            # icon + number centred together — drawn inline, skip ctr_text path
            tw    = len(str(nc)) * 5
            total = _MSG_ICON.width + 2 + tw
            ix    = KEY_SIZE // 2 - total // 2
            iy    = bar_cy - _MSG_ICON.height // 2
            img.paste(_MSG_ICON, (ix, iy), _MSG_ICON)
            draw.text((ix + _MSG_ICON.width + 2, bar_cy), str(nc),
                      font=F_STATUS, fill=(*C_GRAY, 255), anchor="lm")
            ctr_text, ctr_color = None, C_GRAY
        else:
            ctr_text, ctr_color = f"{nc}c", C_GRAY
    else:
        ctr_text, ctr_color = None, C_GRAY
    if ctr_text:
        draw.text((KEY_SIZE // 2, bar_cy), ctr_text,
                  font=F_STATUS, fill=(*ctr_color, 255), anchor="mm")

    # Review indicator (right, vertically centred)
    if rv_l:
        draw.text((KEY_SIZE - 4, bar_cy), rv_l,
                  font=F_STATUS, fill=(*rv_c, 255), anchor="rm")

    path = OUT / f"pr-{idx}.png"
    img.save(str(path))
    return str(path)


def _render_empty_icon(idx):
    img  = Image.new("RGBA", (KEY_SIZE, KEY_SIZE), (*C_BG, 255))
    draw = ImageDraw.Draw(img)
    _rr(draw, 1, 1, KEY_SIZE-2, KEY_SIZE-2, 5, (*C_CARD, 180))
    draw.text((KEY_SIZE//2, KEY_SIZE//2), "—",
              font=F_TITLE, fill=(*C_GRAY, 200), anchor="mm")
    path = OUT / f"pr-{idx}.png"
    img.save(str(path))
    return str(path)


def _render_all(prs):
    """Pre-render every slot image plus header and badge."""
    for i, pr in enumerate(prs[:MAX_PR_SLOTS]):
        _render_pr_icon(pr, i)
    for i in range(len(prs), MAX_PR_SLOTS):
        _render_empty_icon(i)
    _render_header(prs)
    _render_badge(prs)


# ── Header (github.deck key 0) ────────────────────────────────────────────────
def _render_header(prs):
    img  = Image.new("RGBA", (KEY_SIZE, KEY_SIZE), (*C_BG, 255))
    draw = ImageDraw.Draw(img)

    n_total   = len(prs)
    n_pending = sum(1 for p in prs if p["ciState"] == "pending")
    n_fail    = sum(1 for p in prs if p["ciState"] == "failure")
    n_review  = sum(1 for p in prs if p["reviewDecision"] == "changes_requested")

    # Alert glow
    if n_fail or n_review:
        _rr(draw, 0, 0, KEY_SIZE, KEY_SIZE, 6, (*C_RED, 50))
    elif n_pending:
        _rr(draw, 0, 0, KEY_SIZE, KEY_SIZE, 6, (*C_YELLOW, 40))

    # Octocat mark (centred, sized to fill the upper half)
    oc_size = 32
    oc = _OCTOCAT.resize((oc_size, oc_size), Image.LANCZOS)
    img.paste(oc, ((KEY_SIZE - oc_size) // 2, 6), oc)

    # PR count
    c_cnt = (C_RED    if (n_fail or n_review) else
             C_YELLOW if n_pending             else
             C_GREEN  if n_total               else C_GRAY)
    draw.text((KEY_SIZE//2, 40),  str(n_total),  font=F_MED,    fill=(*c_cnt,  255), anchor="mm")
    draw.text((KEY_SIZE//2, 52), "open PRs",     font=F_META,   fill=(*C_GRAY, 255), anchor="mm")

    # Status summary line
    parts = []
    if n_review:  parts.append(f"!{n_review}rev")
    if n_fail:    parts.append(f"x{n_fail}CI")
    if n_pending: parts.append(f"~{n_pending}")
    status = " ".join(parts) if parts else ("all good" if n_total else "")
    c_st   = C_RED if (n_fail or n_review) else C_YELLOW if n_pending else C_GREEN
    if status:
        draw.text((KEY_SIZE//2, 64), status,
                  font=_fnt(_BOLD, 7), fill=(*c_st, 255), anchor="mm")

    path = OUT / "header.png"
    img.save(str(path))
    return str(path)


# ── Badge (main.deck nav button) ─────────────────────────────────────────────
def _render_badge(prs):
    """Render the main.deck nav button.  Visual style matches docker/ports badges:
    geometric icon, count circle top-right, all-caps label bottom."""
    n_total   = len(prs)
    n_pending = sum(1 for p in prs if p["ciState"] == "pending")
    n_fail    = sum(1 for p in prs if p["ciState"] == "failure")
    n_review  = sum(1 for p in prs if p["reviewDecision"] == "changes_requested")
    has_warn  = bool(n_fail or n_review)

    pr_hex  = ("#f85149" if has_warn  else
               "#3fb950" if n_total   else "#6e7681")

    # Top-left badge: total open PRs (always shown)
    br = 10
    lx, ly   = br + 2, br + 2
    lfs      = 10 if n_total >= 10 else 13
    left_badge = (
        f'<circle cx="{lx}" cy="{ly}" r="{br}" fill="{pr_hex}"/>'
        f'<text x="{lx}" y="{ly + lfs // 3}" '
        f'font-family="DejaVu Sans,sans-serif" font-weight="bold" '
        f'font-size="{lfs}" fill="white" text-anchor="middle">{n_total}</text>'
    )

    # Top-right badge: failed CI (red) takes priority over running CI (yellow)
    rx_, ry_ = 72 - br - 2, br + 2
    if n_fail > 0:
        r_count, r_color = n_fail,    "#f85149"
    elif n_pending > 0:
        r_count, r_color = n_pending, "#d29922"
    else:
        r_count, r_color = 0, ""
    rfs = 10 if r_count >= 10 else 13
    right_badge = (
        f'<circle cx="{rx_}" cy="{ry_}" r="{br}" fill="{r_color}"/>'
        f'<text x="{rx_}" y="{ry_ + rfs // 3}" '
        f'font-family="DejaVu Sans,sans-serif" font-weight="bold" '
        f'font-size="{rfs}" fill="white" text-anchor="middle">{r_count}</text>'
    ) if r_count > 0 else ""

    badge_svg = left_badge + right_badge

    # Octocat: head circle + ear tips + eyes + five tentacles
    # All drawn at full #f0f6fc white against the dark background.
    octocat = """
  <!-- ears (drawn first so head overlaps the inner part) -->
  <circle cx="22" cy="18" r="7" fill="#f0f6fc"/>
  <circle cx="50" cy="18" r="7" fill="#f0f6fc"/>
  <!-- inner ear shading -->
  <circle cx="22" cy="18" r="4" fill="#161b22"/>
  <circle cx="50" cy="18" r="4" fill="#161b22"/>
  <!-- head -->
  <circle cx="36" cy="31" r="15" fill="#f0f6fc"/>
  <!-- eyes -->
  <circle cx="30" cy="28" r="3" fill="#0d1117"/>
  <circle cx="42" cy="28" r="3" fill="#0d1117"/>
  <!-- nose -->
  <ellipse cx="36" cy="34" rx="2" ry="1.5" fill="#0d1117" opacity="0.5"/>
  <!-- tentacles (5, alternating lengths) -->
  <ellipse cx="19" cy="52" rx="3" ry="6"  fill="#f0f6fc"/>
  <ellipse cx="27" cy="50" rx="3" ry="7"  fill="#f0f6fc"/>
  <ellipse cx="36" cy="49" rx="3" ry="8"  fill="#f0f6fc"/>
  <ellipse cx="45" cy="50" rx="3" ry="7"  fill="#f0f6fc"/>
  <ellipse cx="53" cy="52" rx="3" ry="6"  fill="#f0f6fc"/>"""

    svg = f"""<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="#0d1117"/>
  {octocat}
  <text x="36" y="69" font-family="DejaVu Sans,sans-serif" font-weight="bold"
        font-size="9" fill="#6b7280" text-anchor="middle">GITHUB</text>
  {badge_svg}
</svg>"""

    path = OUT / "badge.png"
    r = subprocess.run(
        ["bun", "-e",
         f"const sharp=require('sharp');sharp(Buffer.from({json.dumps(svg)})).png()"
         f".toFile({json.dumps(str(path))}).then(()=>process.exit(0))"],
        capture_output=True, cwd=str(Path(__file__).parents[2]),
    )
    if r.returncode != 0:
        # Fallback: PIL render
        img  = Image.new("RGBA", (KEY_SIZE, KEY_SIZE), (*C_BG, 255))
        draw = ImageDraw.Draw(img)
        draw.text((KEY_SIZE//2, KEY_SIZE//2 - 6), "GH",
                  font=F_BIG, fill=(*C_WHITE, 255), anchor="mm")
        draw.text((KEY_SIZE//2, KEY_SIZE - 4), "GITHUB",
                  font=_fnt(_BOLD, 9), fill=(*C_GRAY, 255), anchor="mb")
        img.save(str(path))
    return str(path)


# ── Entry point ───────────────────────────────────────────────────────────────
def main():
    args = sys.argv[1:]
    cmd  = args[0] if args else "badge"

    try:
        prs = get_prs()
    except Exception as e:
        print(f"error fetching PRs: {e}", file=sys.stderr)
        prs = []

    if cmd == "badge":
        path = OUT / "badge.png"
        print(str(path) if path.exists() else _render_badge(prs))

    elif cmd == "header":
        path = OUT / "header.png"
        print(str(path) if path.exists() else _render_header(prs))

    elif cmd == "url":
        n = int(args[1]) if len(args) > 1 else 0
        try:
            cached = json.loads(CACHE_FILE.read_text())
            url = cached["prs"][n].get("url", "") if n < len(cached["prs"]) else ""
            if url:
                _snooze()
                subprocess.Popen(
                    ["xdg-open", url],
                    env={**os.environ, "DISPLAY": os.environ.get("DISPLAY", ":1")},
                )
        except Exception as e:
            print(f"error opening PR: {e}", file=sys.stderr)
            sys.exit(1)

    elif cmd == "icon":
        n    = int(args[1]) if len(args) > 1 else 0
        path = OUT / f"pr-{n}.png"
        if not path.exists():
            if n < len(prs):
                _render_pr_icon(prs[n], n)
            else:
                _render_empty_icon(n)
        print(str(path))

    else:
        print(f"unknown command: {cmd}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
