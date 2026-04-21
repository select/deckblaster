#!/usr/bin/env python3
"""
Polymarket trending bets viewer for Stream Deck MK.2.

Layout (5×3) — 3 bets per page, each bet is a vertical column:
  [INFO  ] [YES 1 ] [YES 2 ] [YES 3 ] [RELOAD]
  [ ◀    ] [TXT 1 ] [TXT 2 ] [TXT 3 ] [  ▶   ]
  [EXIT  ] [NO  1 ] [NO  2 ] [NO  3 ] [      ]

Middle text keys are clickable — press to see more text.

Commands: init, next, prev, reload, more <0|1|2>
"""

import glob
import json
import math
import os
import sys
import textwrap
import time
import urllib.request
from PIL import Image, ImageDraw, ImageFont

API = "http://localhost:9990"
GAMMA_API = "https://gamma-api.polymarket.com"
# Persistent storage (survives reboot)
DATA_DIR = os.path.expanduser("~/.local/share/streamdeck-polymarket")
os.makedirs(DATA_DIR, exist_ok=True)
STATE_FILE = os.path.join(DATA_DIR, "state.json")
CATS_CACHE_FILE = os.path.join(DATA_DIR, "categories.json")

# Ephemeral cache + rendered images (ok to lose on reboot)
CACHE_FILE = "/tmp/streamdeck-polymarket-cache.json"
TMP = "/tmp/streamdeck-polymarket"
CACHE_TTL = 300
os.makedirs(TMP, exist_ok=True)

SIZE = 72
BETS_PER_PAGE = 3
TOTAL_FETCH = 60

# Only sports/junk tags are excluded — everything else comes from the API.
# No predefined category list. "Popular" is the only hardcoded entry.
SPORTS_TAGS = {
    'sports','soccer','nba','nfl','mlb','nhl','tennis','golf','mma','ufc','boxing',
    'epl','la liga','serie a','bundesliga','ligue 1','champions league','f1',
    'college football playoffs','cricket','major league cricket','hockey',
    'europa league','investec champions cup','ecf mvp','nl central',
    'ohtani','qb','green bay packers','caitlin clark','florida panthers',
    'san jose sharks','mavericks','bat','egypt premier league',
    'nba finals','basketball','nba champion','mvp','stanley cup',
    'ucl','fifa world cup','2026 fifa world cup',
}
SKIP_TAGS = {
    'earn 4%', 'hide from new', 'pre-market', 'token launch',
    'governor midterms', 'senate midterms', '2025 predictions',
    'product marekt fit', 'virgins', 'macro election 2',
}
CATS_CACHE_FILE = os.path.join(
    os.path.expanduser("~/.local/share/streamdeck-polymarket"), "categories.json"
)
CATS_PER_PAGE_SEL = 9

BET_COLS = [
    (1, 6, 11),
    (2, 7, 12),
    (3, 8, 13),
]
TEXT_KEYS = [6, 7, 8]
KEY_INFO = 0
KEY_PREV = 5
KEY_NEXT = 9
KEY_RELOAD = 4
KEY_CATEGORY = 14
KEY_EXIT = 10

# Text layout
FONT_SIZE_TEXT = 12
MAX_LINES = 4
WRAP_WIDTH = 9

try:
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 26)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 10)
    FONT_TXT = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", FONT_SIZE_TEXT)
    FONT_XS = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 9)
except Exception:
    FONT_LG = FONT_MD = FONT_SM = FONT_TXT = FONT_XS = ImageFont.load_default()


def load_state():
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE) as f:
                return json.load(f)
        except (json.JSONDecodeError, IOError):
            pass
    return {"page": 0, "total": 0, "frames": [0, 0, 0], "cat_idx": 0}


def save_state(state):
    """Atomic write — write to temp file then rename to avoid partial reads."""
    tmp = STATE_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(state, f)
    os.replace(tmp, STATE_FILE)


def push_icon(key, path):
    data = json.dumps({"icon": path}).encode()
    req = urllib.request.Request(f"{API}/key/{key}", data=data,
                                headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=3)
    except Exception as e:
        print(f"API error key {key}: {e}", file=sys.stderr)


def fetch_markets(force=False, query=None, tag_id=None):
    """Fetch markets. Uses tag_id filtering (fast) or search query (fallback)."""
    cache_key = tag_id or query or "popular"
    cache_path = CACHE_FILE.replace(".json", f"-{str(cache_key).replace(' ','_')}.json")

    if not force and os.path.exists(cache_path):
        try:
            with open(cache_path) as f:
                cache = json.load(f)
            if time.time() - cache.get("ts", 0) < CACHE_TTL:
                return cache["markets"]
        except Exception:
            pass

    markets = []
    try:
        if tag_id:
            # Best method: filter events by tag_id, extract markets
            url = f"{GAMMA_API}/events?active=true&closed=false&tag_id={tag_id}&limit={TOTAL_FETCH}&order=volume_24hr&ascending=false"
            req = urllib.request.Request(url, headers={"User-Agent": "StreamDeck/1.0"})
            resp = urllib.request.urlopen(req, timeout=10)
            events = json.loads(resp.read().decode())
            raw_markets = []
            for event in events:
                for m in event.get("markets", []):
                    raw_markets.append(m)
        elif query:
            # Fallback: search endpoint
            q = urllib.request.quote(query)
            url = f"{GAMMA_API}/public-search?q={q}&limit={TOTAL_FETCH}"
            req = urllib.request.Request(url, headers={"User-Agent": "StreamDeck/1.0"})
            resp = urllib.request.urlopen(req, timeout=10)
            data = json.loads(resp.read().decode())
            raw_markets = []
            for event in data.get("events", []):
                for m in event.get("markets", []):
                    raw_markets.append(m)
        else:
            url = f"{GAMMA_API}/markets?active=true&closed=false&limit={TOTAL_FETCH}&order=volume_24hr&ascending=false"
            req = urllib.request.Request(url, headers={"User-Agent": "StreamDeck/1.0"})
            resp = urllib.request.urlopen(req, timeout=10)
            raw_markets = json.loads(resp.read().decode())

        for m in raw_markets:
            try:
                prices = json.loads(m.get("outcomePrices", "[]"))
                if not prices:
                    continue
                yes_price = float(prices[0])
                if yes_price <= 0.02 or yes_price >= 0.98:
                    continue
                markets.append({
                    "question": m.get("question", "?"),
                    "yes": yes_price,
                    "no": 1.0 - yes_price,
                    "volume": float(m.get("volume", 0)),
                })
            except (ValueError, IndexError, KeyError):
                continue

        # Sort by volume
        markets.sort(key=lambda x: x["volume"], reverse=True)

    except Exception as e:
        print(f"Fetch error: {e}", file=sys.stderr)
        if os.path.exists(cache_path):
            with open(cache_path) as f:
                return json.load(f).get("markets", [])
        return []

    with open(cache_path, "w") as f:
        json.dump({"ts": time.time(), "markets": markets}, f)
    return markets


def format_volume(v):
    if v >= 1_000_000:
        return f"${v / 1_000_000:.1f}M"
    elif v >= 1_000:
        return f"${v / 1_000:.0f}K"
    return f"${v:.0f}"


def render_yes(market, idx):
    img = Image.new("RGBA", (SIZE, SIZE), (8, 18, 8, 255))
    draw = ImageDraw.Draw(img)
    yes = market["yes"]
    pct = int(yes * 100)

    bar_h = int(60 * yes)
    for y in range(SIZE - 4 - bar_h, SIZE - 4):
        t = (y - (SIZE - 4 - bar_h)) / max(bar_h, 1)
        g = int(80 + 80 * t)
        draw.line([(4, y), (SIZE - 4, y)], fill=(int(15 + 15 * t), g, int(25 + 25 * t)))

    draw.text((6, 4), "YES", fill=(100, 220, 120), font=FONT_SM)
    pct_text = f"{pct}%"
    bbox = draw.textbbox((0, 0), pct_text, font=FONT_LG)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
    draw.text(((SIZE - tw) // 2, (SIZE - th) // 2 + 2), pct_text, fill=(150, 255, 160), font=FONT_LG)
    draw.rounded_rectangle([1, 1, 71, 71], radius=6, outline=(40, 100, 50), width=1)

    path = os.path.join(TMP, f"yes-{idx}.png")
    img.save(path)
    return path


def render_text(market, idx, frame=0):
    img = Image.new("RGBA", (SIZE, SIZE), (12, 12, 20, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([1, 1, 71, 71], radius=6, outline=(50, 50, 70), width=1)

    question = market["question"]
    volume = market["volume"]

    all_lines = textwrap.wrap(question, width=WRAP_WIDTH)
    total_frames = max(1, (len(all_lines) + MAX_LINES - 1) // MAX_LINES)
    frame = frame % total_frames
    start_line = frame * MAX_LINES
    visible = all_lines[start_line:start_line + MAX_LINES]

    y = 4
    for line in visible:
        draw.text((4, y), line, fill=(220, 220, 240), font=FONT_TXT)
        y += 14

    # Page dots if multi-frame
    if total_frames > 1:
        dot_y = 56
        dot_x_start = (SIZE - total_frames * 8) // 2
        for i in range(total_frames):
            c = (100, 160, 255) if i == frame else (40, 40, 60)
            draw.ellipse([dot_x_start + i * 8, dot_y, dot_x_start + i * 8 + 5, dot_y + 5], fill=c)

    # Volume at bottom
    vol_text = format_volume(volume)
    bbox = draw.textbbox((0, 0), vol_text, font=FONT_XS)
    tw = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 62), vol_text, fill=(120, 120, 160), font=FONT_XS)

    path = os.path.join(TMP, f"text-{idx}.png")
    img.save(path)
    return path


def render_no(market, idx):
    img = Image.new("RGBA", (SIZE, SIZE), (18, 8, 8, 255))
    draw = ImageDraw.Draw(img)
    no = market["no"]
    pct = int(no * 100)

    bar_h = int(60 * no)
    for y in range(4, 4 + bar_h):
        t = (y - 4) / max(bar_h, 1)
        draw.line([(4, y), (SIZE - 4, y)], fill=(int(160 - 80 * t), int(30 - 15 * t), int(30 - 15 * t)))

    pct_text = f"{pct}%"
    bbox = draw.textbbox((0, 0), pct_text, font=FONT_LG)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
    draw.text(((SIZE - tw) // 2, (SIZE - th) // 2 - 2), pct_text, fill=(255, 140, 140), font=FONT_LG)
    draw.text((48, 56), "NO", fill=(220, 100, 100), font=FONT_SM)
    draw.rounded_rectangle([1, 1, 71, 71], radius=6, outline=(100, 40, 40), width=1)

    path = os.path.join(TMP, f"no-{idx}.png")
    img.save(path)
    return path


def render_empty():
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    path = os.path.join(TMP, "empty-bet.png")
    img.save(path)
    return path


def render_info(page, total_pages, total_markets, cat_name):
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(15, 15, 30), outline=(60, 80, 140), width=2)
    pg_text = f"{page + 1}/{total_pages}"
    bbox = draw.textbbox((0, 0), pg_text, font=FONT_MD)
    tw = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 4), pg_text, fill=(100, 160, 255), font=FONT_MD)
    # Category name
    bbox2 = draw.textbbox((0, 0), cat_name, font=FONT_SM)
    tw2 = bbox2[2] - bbox2[0]
    draw.text(((SIZE - tw2) // 2, 24), cat_name, fill=(200, 160, 255), font=FONT_SM)
    draw.text((6, 40), "Polymarket", fill=(80, 80, 120), font=FONT_XS)
    draw.text((6, 56), f"{total_markets} bets", fill=(100, 100, 140), font=FONT_XS)
    path = os.path.join(TMP, "info.png")
    img.save(path)
    return path



def clear_old_images():
    """Remove stale rendered bet images so old content never shows."""
    for prefix in ("bet-", "text-", "yes-", "no-", "info"):
        for f in glob.glob(os.path.join(TMP, f"{prefix}*.png")):
            try:
                os.remove(f)
            except OSError:
                pass


def set_loading(state):
    """Mark state as loading so text renderers show empty."""
    state["loading"] = True
    save_state(state)
    clear_old_images()


def clear_loading(state):
    state["loading"] = False
    save_state(state)


def get_category(state):
    """Get current category name, query, and tag_id from state."""
    name = state.get("cat_name", "Popular")
    query = state.get("cat_query", None)
    tag_id = state.get("cat_tag_id", None)
    return (name, query, tag_id)


def display_page(state, markets):
    page = state.get("page", 0)
    total_pages = max(1, (len(markets) + BETS_PER_PAGE - 1) // BETS_PER_PAGE)
    page = max(0, min(page, total_pages - 1))
    state["page"] = page
    state["total"] = len(markets)
    state["frames"] = [0, 0, 0]

    cat_name, _, _ = get_category(state)
    # Render info image (icon_command on key 0 will pick it up)
    render_info(page, total_pages, len(markets), cat_name)

    start = page * BETS_PER_PAGE
    for i, (key_yes, key_txt, key_no) in enumerate(BET_COLS):
        idx = start + i
        if idx < len(markets):
            m = markets[idx]
            render_yes(m, i)
            render_no(m, i)
        else:
            empty = render_empty()
            # Write empty images for icon_command to pick up
            import shutil
            for dest in [os.path.join(TMP, f"yes-{i}.png"), os.path.join(TMP, f"no-{i}.png")]:
                shutil.copy2(empty, dest)


def cmd_init():
    state = load_state()
    set_loading(state)
    _, query, tag_id = get_category(state)
    markets = fetch_markets(query=query, tag_id=tag_id)
    display_page(state, markets)
    clear_loading(state)


def cmd_next():
    state = load_state()
    set_loading(state)
    _, query, tag_id = get_category(state)
    markets = fetch_markets(query=query, tag_id=tag_id)
    total_pages = max(1, (len(markets) + BETS_PER_PAGE - 1) // BETS_PER_PAGE)
    state["page"] = min(state.get("page", 0) + 1, total_pages - 1)
    display_page(state, markets)
    clear_loading(state)


def cmd_prev():
    state = load_state()
    set_loading(state)
    _, query, tag_id = get_category(state)
    markets = fetch_markets(query=query, tag_id=tag_id)
    state["page"] = max(state.get("page", 0) - 1, 0)
    display_page(state, markets)
    clear_loading(state)


def cmd_reload():
    state = load_state()
    state["page"] = 0
    set_loading(state)
    # Force-refresh both categories and markets
    build_categories(state, force=True)
    _, query, tag_id = get_category(state)
    markets = fetch_markets(force=True, query=query, tag_id=tag_id)
    display_page(state, markets)
    clear_loading(state)


CATS_CACHE_TTL = 600  # 10 min — categories change slowly

def build_categories(state, force=False):
    """Build category list from API: discover popular tags from active events.
    Returns cached version if fresh enough (unless force=True)."""
    if not force and os.path.exists(CATS_CACHE_FILE):
        try:
            mtime = os.path.getmtime(CATS_CACHE_FILE)
            if time.time() - mtime < CATS_CACHE_TTL:
                with open(CATS_CACHE_FILE) as f:
                    return json.load(f)
        except Exception:
            pass

    visits = state.get("cat_visits", {})

    # Always start with "Popular" (unfiltered)
    cats = [{"name": "Popular", "tag_id": None, "query": None, "visits": visits.get("Popular", 0)}]
    seen = {"popular"}

    try:
        tag_data = {}  # label -> {id, count, vol24h}
        for offset in range(0, 1000, 200):
            url = f"{GAMMA_API}/events?active=true&closed=false&limit=200&offset={offset}&order=volume_24hr&ascending=false"
            req = urllib.request.Request(url, headers={"User-Agent": "StreamDeck/1.0"})
            resp = urllib.request.urlopen(req, timeout=15)
            events = json.loads(resp.read().decode())
            if not events:
                break
            for e in events:
                v24 = float(e.get("volume24hr", 0))
                for t in e.get("tags", []):
                    label = t.get("label", "")
                    tid = t.get("id", "")
                    if not label:
                        continue
                    if label.lower() in SPORTS_TAGS or label.lower() in SKIP_TAGS:
                        continue
                    if label not in tag_data:
                        tag_data[label] = {"id": tid, "count": 0, "vol24h": 0}
                    tag_data[label]["count"] += 1
                    tag_data[label]["vol24h"] += v24

        # Add tags with 2+ events, sorted by 24h volume
        for label, d in sorted(tag_data.items(), key=lambda x: -x[1]["vol24h"]):
            if d["count"] >= 2 and label.lower() not in seen:
                cats.append({
                    "name": label,
                    "tag_id": d["id"],
                    "query": label,
                    "count": d["count"],
                    "visits": visits.get(label, 0),
                })
                seen.add(label.lower())
    except Exception as e:
        print(f"Category discovery error: {e}", file=sys.stderr)

    # Sort: most visited first, then by bet count (descending)
    # Keep Popular always first, cap at 50 pages
    MAX_CATS = 50 * CATS_PER_PAGE_SEL  # 450
    popular = cats[0]
    rest = cats[1:]
    rest.sort(key=lambda c: (-c["visits"], -c.get("count", 0)))
    cats = [popular] + rest[:MAX_CATS - 1]

    with open(CATS_CACHE_FILE, "w") as f:
        json.dump(cats, f)
    return cats


def cmd_category():
    """Unused legacy — now use cat-init/select-cat."""
    pass


def _update_cats_visits(state):
    """Update visit counts in cached categories and re-sort without API call."""
    if not os.path.exists(CATS_CACHE_FILE):
        return
    with open(CATS_CACHE_FILE) as f:
        cats = json.load(f)
    visits = state.get("cat_visits", {})
    for cat in cats:
        cat["visits"] = visits.get(cat["name"], 0)
    # Re-sort: Popular first, then visited first, then by bet count
    popular = cats[0] if cats and cats[0]["name"] == "Popular" else None
    rest = [c for c in cats if c["name"] != "Popular"]
    rest.sort(key=lambda c: (-c["visits"], -c.get("count", 0)))
    cats = ([popular] if popular else []) + rest
    with open(CATS_CACHE_FILE, "w") as f:
        json.dump(cats, f)


def _render_cat_icons():
    """Pre-render all 9 category icons in one process."""
    import subprocess
    subprocess.run([sys.executable,
                    os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                 "polymarket-cat-render-all.py")],
                   capture_output=True)


def cmd_cat_init():
    """Initialize the category selection page (only on first load)."""
    state = load_state()
    # Don't reset cat_page — preserve pagination across re-polls
    build_categories(state)
    save_state(state)
    _render_cat_icons()


def cmd_select_cat(slot):
    """Select a category from the category page by slot number."""
    state = load_state()

    if not os.path.exists(CATS_CACHE_FILE):
        return
    with open(CATS_CACHE_FILE) as f:
        cats = json.load(f)

    cat_page = state.get("cat_page", 0)
    total_pages = max(1, (len(cats) + CATS_PER_PAGE_SEL - 1) // CATS_PER_PAGE_SEL)
    cat_page = cat_page % total_pages
    start = cat_page * CATS_PER_PAGE_SEL
    page_cats = cats[start:start + CATS_PER_PAGE_SEL]

    if slot >= len(page_cats):
        return

    cat = page_cats[slot]
    name = cat["name"]

    # Update selection and visit count
    state["cat_name"] = name
    state["cat_query"] = cat.get("query")
    state["cat_tag_id"] = cat.get("tag_id")
    state["page"] = 0
    state["frames"] = [0, 0, 0]
    visits = state.get("cat_visits", {})
    visits[name] = visits.get(name, 0) + 1
    state["cat_visits"] = visits
    save_state(state)

    # Update visits in cached categories and re-sort (no API call)
    _update_cats_visits(state)


def cmd_cat_next():
    state = load_state()
    if os.path.exists(CATS_CACHE_FILE):
        with open(CATS_CACHE_FILE) as f:
            cats = json.load(f)
        total_pages = max(1, (len(cats) + CATS_PER_PAGE_SEL - 1) // CATS_PER_PAGE_SEL)
        state["cat_page"] = min(state.get("cat_page", 0) + 1, total_pages - 1)
    save_state(state)
    _render_cat_icons()


def cmd_cat_prev():
    state = load_state()
    state["cat_page"] = max(state.get("cat_page", 0) - 1, 0)
    save_state(state)
    _render_cat_icons()


def cmd_cat_home():
    state = load_state()
    state["cat_page"] = 0
    save_state(state)
    _render_cat_icons()


def cmd_more(slot):
    """Advance the text frame for one bet slot (0, 1, or 2)."""
    state = load_state()
    _, query, tag_id = get_category(state)
    markets = fetch_markets(query=query, tag_id=tag_id)
    if not markets:
        return

    page = state.get("page", 0)
    start = page * BETS_PER_PAGE
    idx = start + slot

    if idx >= len(markets):
        return

    frames = state.get("frames", [0, 0, 0])
    while len(frames) < 3:
        frames.append(0)

    # Monospace grid: 9 cols x 4 rows, scroll by 2 lines
    GRID_COLS = 9
    GRID_ROWS = 4
    SCROLL_STEP = 1

    words = markets[idx]["question"].split()
    lines = []
    current = ""
    for w in words:
        if not current:
            current = w
        elif len(current) + 1 + len(w) <= GRID_COLS:
            current += " " + w
        else:
            lines.append(current)
            current = w
    if current:
        lines.append(current)

    max_scroll = max(0, len(lines) - GRID_ROWS)
    total_steps = max(1, math.ceil(max_scroll / SCROLL_STEP) + 1) if max_scroll > 0 else 1
    if frames[slot] >= total_steps - 1:
        frames[slot] = 0
    else:
        frames[slot] = frames[slot] + 1

    state["frames"] = frames
    save_state(state)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: polymarket.py {init|next|prev|reload|more <0|1|2>}")
        sys.exit(1)

    cmd = sys.argv[1]
    if cmd == "more" and len(sys.argv) >= 3:
        cmd_more(int(sys.argv[2]))
    elif cmd == "select-cat" and len(sys.argv) >= 3:
        cmd_select_cat(int(sys.argv[2]))
    else:
        cmds = {
            "init": cmd_init, "next": cmd_next, "prev": cmd_prev,
            "reload": cmd_reload, "cat-init": cmd_cat_init,
            "cat-next": cmd_cat_next, "cat-prev": cmd_cat_prev, "cat-home": cmd_cat_home,
        }
        fn = cmds.get(cmd)
        if fn:
            fn()
        else:
            print(f"Unknown: {cmd}", file=sys.stderr)
            sys.exit(1)
