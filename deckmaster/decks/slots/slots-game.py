#!/usr/bin/env python3
"""
Slot machine game for Stream Deck MK.2.

Uses the deckmaster HTTP API (:9990) to push images to keys.
State is persisted in /tmp/streamdeck-slots.json.

Layout (5x3 grid):
  [CREDIT] [reel ] [reel ] [reel ] [  BET ]
  [ SPIN ] [reel ] [reel ] [reel ] [ WIN$ ]
  [ EXIT ] [reel ] [reel ] [reel ] [LINES ]

Reel keys: 1,2,3 (top), 6,7,8 (mid), 11,12,13 (bot)
"""

import json
import os
import random
import sys
import time
import urllib.request
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont, ImageFilter

API = "http://localhost:9990"
ASSET = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
STATE_FILE = "/tmp/streamdeck-slots.json"
TMP = "/tmp/streamdeck-slots"
os.makedirs(TMP, exist_ok=True)

SIZE = 72

SYMBOLS = ["cherry", "lemon", "bell", "diamond", "seven", "star", "bar"]
# Weights: seven and diamond are rarer (higher value)
WEIGHTS = [20, 20, 15, 10, 5, 15, 15]  # cherry, lemon, bell, diamond, seven, star, bar

# Payouts for 3-of-a-kind on any line
PAYOUTS = {
    "cherry": 5,
    "lemon": 5,
    "bell": 10,
    "star": 15,
    "bar": 20,
    "diamond": 40,
    "seven": 77,
}

# 2-of-a-kind pays half (only on middle row)
PAYOUT_2 = {k: max(1, v // 3) for k, v in PAYOUTS.items()}

# Grid key indices (row, col) -> Stream Deck key index
GRID = {
    (0, 0): 1, (0, 1): 2, (0, 2): 3,
    (1, 0): 6, (1, 1): 7, (1, 2): 8,
    (2, 0): 11, (2, 1): 12, (2, 2): 13,
}

# UI key indices
KEY_CREDITS = 0
KEY_SPIN = 5
KEY_EXIT = 10
KEY_BET = 4
KEY_WIN = 9
KEY_LINES = 14

# Winning lines: list of [(row,col), ...] triples
WIN_LINES = [
    # Horizontal
    [(0, 0), (0, 1), (0, 2)],  # top
    [(1, 0), (1, 1), (1, 2)],  # middle
    [(2, 0), (2, 1), (2, 2)],  # bottom
    # Diagonal
    [(0, 0), (1, 1), (2, 2)],  # top-left to bottom-right
    [(2, 0), (1, 1), (0, 2)],  # bottom-left to top-right
]

try:
    FONT = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 14)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 18)
    FONT_XL = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 28)
except:
    FONT = FONT_SM = FONT_LG = FONT_XL = ImageFont.load_default()


def load_state():
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            return json.load(f)
    return {"credits": 100, "bet": 5, "grid": None, "spinning": False}


def save_state(state):
    with open(STATE_FILE, "w") as f:
        json.dump(state, f)


def push_icon(key, icon_path):
    """Push an icon image to a Stream Deck key via HTTP API."""
    data = json.dumps({"icon": icon_path}).encode()
    req = urllib.request.Request(f"{API}/key/{key}", data=data,
                                 headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=2)
    except Exception as e:
        print(f"API error key {key}: {e}", file=sys.stderr)


def push_text(key, label, color="#ffffff", bg="#000000", fontsize=12):
    """Push text to a key."""
    data = json.dumps({
        "label": label, "color": color, "background": bg, "fontsize": fontsize
    }).encode()
    req = urllib.request.Request(f"{API}/key/{key}", data=data,
                                 headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=2)
    except Exception as e:
        print(f"API error key {key}: {e}", file=sys.stderr)


def render_text_image(lines, colors=None, bg_color=(10, 10, 20)):
    """Render multi-line text to a 72x72 image, return path."""
    img = Image.new("RGBA", (SIZE, SIZE), bg_color + (255,))
    draw = ImageDraw.Draw(img)

    if colors is None:
        colors = ["#ffffff"] * len(lines)

    total_h = len(lines) * 16
    start_y = (SIZE - total_h) // 2

    for i, (text, color) in enumerate(zip(lines, colors)):
        font = FONT_SM if len(text) > 6 else FONT
        bbox = draw.textbbox((0, 0), text, font=font)
        tw = bbox[2] - bbox[0]
        x = (SIZE - tw) // 2
        y = start_y + i * 16
        draw.text((x, y), text, fill=color, font=font)

    path = os.path.join(TMP, f"txt-{hash(tuple(lines)) % 100000}.png")
    img.save(path)
    return path


def render_credits_image(credits):
    img = Image.new("RGBA", (SIZE, SIZE), (10, 10, 30, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(60, 60, 120), width=2)

    draw.text((12, 8), "CREDIT", fill=(120, 120, 200), font=FONT_SM)

    text = str(credits)
    font = FONT_XL if len(text) <= 3 else FONT_LG
    bbox = draw.textbbox((0, 0), text, font=font)
    tw = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 30), text, fill=(255, 255, 100), font=font)

    path = os.path.join(TMP, "credits.png")
    img.save(path)
    return path


def render_bet_image(bet):
    img = Image.new("RGBA", (SIZE, SIZE), (10, 10, 30, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(100, 60, 60), width=2)

    draw.text((18, 8), "BET", fill=(200, 120, 120), font=FONT_SM)

    text = str(bet)
    bbox = draw.textbbox((0, 0), text, font=FONT_XL)
    tw = bbox[2] - bbox[0]
    draw.text(((SIZE - tw) // 2, 30), text, fill=(255, 150, 80), font=FONT_XL)

    path = os.path.join(TMP, "bet.png")
    img.save(path)
    return path


def render_win_image(amount):
    img = Image.new("RGBA", (SIZE, SIZE), (10, 10, 30, 255))
    draw = ImageDraw.Draw(img)

    if amount > 0:
        draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(255, 215, 0), width=3)
        draw.text((16, 6), "WIN!", fill=(255, 215, 0), font=FONT_SM)
        text = f"+{amount}"
        font = FONT_LG if len(text) <= 4 else FONT
        bbox = draw.textbbox((0, 0), text, font=font)
        tw = bbox[2] - bbox[0]
        draw.text(((SIZE - tw) // 2, 32), text, fill=(100, 255, 100), font=font)
    else:
        draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(40, 40, 60), width=2)
        draw.text((16, 28), "WIN", fill=(60, 60, 80), font=FONT)

    path = os.path.join(TMP, f"win-{amount}.png")
    img.save(path)
    return path


def render_lines_image():
    img = Image.new("RGBA", (SIZE, SIZE), (10, 10, 30, 255))
    draw = ImageDraw.Draw(img)
    draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(40, 40, 60), width=2)
    draw.text((10, 8), "LINES", fill=(120, 120, 160), font=FONT_SM)
    draw.text((22, 30), "5", fill=(180, 180, 255), font=FONT_XL)
    path = os.path.join(TMP, "lines.png")
    img.save(path)
    return path


def pick_symbol():
    return random.choices(SYMBOLS, weights=WEIGHTS, k=1)[0]


def render_spinning_frame(col, frame_idx):
    """Create a spinning blur frame with symbol hints."""
    img = Image.new("RGBA", (SIZE, SIZE), (15, 15, 25, 255))
    draw = ImageDraw.Draw(img)

    # Fast-scrolling colored streaks
    random.seed(frame_idx * 31 + col * 7)
    colors = [
        (220, 20, 20), (255, 220, 40), (60, 140, 255),
        (255, 215, 0), (0, 200, 50), (180, 0, 220), (255, 150, 80)
    ]
    for y in range(0, SIZE, 4):
        c = random.choice(colors)
        alpha = random.randint(60, 180)
        c_with_alpha = c + (alpha,)
        w = random.randint(20, 56)
        x_start = random.randint(4, SIZE - w - 4)
        draw.rounded_rectangle([x_start, y, x_start + w, y + 3], radius=1, fill=c_with_alpha)

    # Subtle border
    draw.rounded_rectangle([1, 1, 71, 71], radius=3, outline=(80, 80, 100, 120), width=1)

    path = os.path.join(TMP, f"spinning-{col}-{frame_idx}.png")
    img.save(path)
    return path


def render_glitter_overlay(symbol_path, frame_idx):
    """Composite glitter on top of a symbol image."""
    base = Image.open(symbol_path).convert("RGBA")

    overlay = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(overlay)

    random.seed(frame_idx * 137)

    sparkle_colors = [
        (255, 255, 100), (255, 215, 0), (255, 255, 255),
        (255, 180, 50), (200, 255, 200), (180, 220, 255),
        (255, 150, 150), (150, 255, 150), (150, 150, 255),
    ]

    # Falling glitter particles
    for _ in range(25):
        x = random.randint(0, SIZE - 1)
        y = random.randint(0, SIZE - 1)
        r = random.randint(1, 3)
        c = random.choice(sparkle_colors)
        a = random.randint(120, 255)
        draw.ellipse([x - r, y - r, x + r, y + r], fill=c + (a,))

    # Golden border flash
    border_alpha = int(180 + 75 * (0.5 + 0.5 * (frame_idx % 3 - 1)))
    border_alpha = max(0, min(255, border_alpha))
    draw.rounded_rectangle([1, 1, 71, 71], radius=4,
                           outline=(255, 215, 0, border_alpha), width=3)

    result = Image.alpha_composite(base, overlay)
    path = os.path.join(TMP, f"glitter-{frame_idx}-{hash(symbol_path) % 10000}.png")
    result.save(path)
    return path


def render_big_win_frame(amount, frame_idx):
    """Animated big win display for the WIN key."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)

    # Pulsing gold background
    pulse = 0.5 + 0.5 * ((frame_idx % 4) / 3.0)
    r = int(80 * pulse)
    g = int(60 * pulse)
    draw.rounded_rectangle([2, 2, 70, 70], radius=8, fill=(r, g, 0),
                           outline=(255, 215, 0), width=3)

    draw.text((14, 6), "WIN!", fill=(255, 255, 100), font=FONT)

    text = f"+{amount}"
    font = FONT_LG if len(text) <= 4 else FONT
    bbox = draw.textbbox((0, 0), text, font=font)
    tw = bbox[2] - bbox[0]
    y_offset = 4 * (frame_idx % 2)
    draw.text(((SIZE - tw) // 2, 30 + y_offset), text, fill=(100, 255, 100), font=font)

    # Sparkles
    random.seed(frame_idx * 99)
    for _ in range(10):
        x = random.randint(5, 67)
        y = random.randint(5, 67)
        c = random.choice([(255, 255, 255), (255, 215, 0), (255, 255, 150)])
        draw.ellipse([x-1, y-1, x+1, y+1], fill=c)

    path = os.path.join(TMP, f"bigwin-{frame_idx}.png")
    img.save(path)
    return path


def show_ui(state):
    """Update info-only keys (credits + win). Never touch action keys (SPIN/EXIT/BET/LINES)
    because the HTTP API replaces the widget and destroys its deck action binding."""
    push_icon(KEY_CREDITS, render_credits_image(state["credits"]))
    push_icon(KEY_WIN, render_win_image(0))


def show_grid(grid):
    """Display the 3x3 symbol grid."""
    for (r, c), key in GRID.items():
        symbol = grid[r][c]
        push_icon(key, os.path.join(ASSET, f"{symbol}.png"))


def check_wins(grid, bet):
    """Check all win lines, return (total_win, winning_positions)."""
    total = 0
    winners = set()

    for line in WIN_LINES:
        syms = [grid[r][c] for r, c in line]
        if syms[0] == syms[1] == syms[2]:
            # 3-of-a-kind
            payout = PAYOUTS.get(syms[0], 5) * bet
            total += payout
            for pos in line:
                winners.add(pos)
        elif syms[0] == syms[1] or syms[1] == syms[2]:
            # 2-of-a-kind (partial match) - small payout
            matching = syms[0] if syms[0] == syms[1] else syms[1]
            payout = PAYOUT_2.get(matching, 1) * bet
            total += payout
            if syms[0] == syms[1]:
                winners.add(line[0])
                winners.add(line[1])
            else:
                winners.add(line[1])
                winners.add(line[2])

    return total, winners


def animate_spin(state):
    """Animate the slot machine spin with cascading column stops."""
    grid = [[None, None, None] for _ in range(3)]
    bet = state["bet"]

    # Deduct bet
    state["credits"] -= bet
    save_state(state)
    push_icon(KEY_CREDITS, render_credits_image(state["credits"]))

    # Pre-determine results
    for r in range(3):
        for c in range(3):
            grid[r][c] = pick_symbol()

    # Phase 1: All columns spinning (fast)
    for frame in range(8):
        for col in range(3):
            path = render_spinning_frame(col, frame)
            for row in range(3):
                push_icon(GRID[(row, col)], path)
        time.sleep(0.08)

    # Phase 2: Column 0 slows down and stops
    for frame in range(4):
        path = render_spinning_frame(0, frame + 20)
        for row in range(3):
            push_icon(GRID[(row, 0)], path)
        time.sleep(0.12)

    # Stop column 0 - reveal with a bounce
    for row in range(3):
        push_icon(GRID[(row, 0)], os.path.join(ASSET, f"{grid[row][0]}.png"))
    time.sleep(0.05)

    # Column 1 keeps spinning then stops
    for frame in range(3):
        path = render_spinning_frame(1, frame + 30)
        for row in range(3):
            push_icon(GRID[(row, 1)], path)
        # Column 2 also still spinning
        path2 = render_spinning_frame(2, frame + 40)
        for row in range(3):
            push_icon(GRID[(row, 2)], path2)
        time.sleep(0.12)

    # Stop column 1
    for row in range(3):
        push_icon(GRID[(row, 1)], os.path.join(ASSET, f"{grid[row][1]}.png"))
    time.sleep(0.05)

    # Column 2 final spins (dramatic slowdown)
    for frame in range(4):
        path = render_spinning_frame(2, frame + 50)
        for row in range(3):
            push_icon(GRID[(row, 2)], path)
        time.sleep(0.15 + frame * 0.05)

    # Stop column 2
    for row in range(3):
        push_icon(GRID[(row, 2)], os.path.join(ASSET, f"{grid[row][2]}.png"))

    time.sleep(0.15)

    # Check wins
    winnings, winners = check_wins(grid, bet)

    if winnings > 0:
        # WIN animation — glitter on winning symbols
        state["credits"] += winnings

        # Animate winning keys with glitter + pulsing win display
        for cycle in range(12):
            # Flash winning symbols with glitter
            for (r, c) in winners:
                symbol = grid[r][c]
                symbol_path = os.path.join(ASSET, f"{symbol}.png")
                glitter_path = render_glitter_overlay(symbol_path, cycle)
                push_icon(GRID[(r, c)], glitter_path)

            # Animate win display
            win_path = render_big_win_frame(winnings, cycle)
            push_icon(KEY_WIN, win_path)

            # Update credits with animation
            if cycle == 4:
                push_icon(KEY_CREDITS, render_credits_image(state["credits"]))

            time.sleep(0.12)

        # Settle to win variant images
        for (r, c) in winners:
            symbol = grid[r][c]
            push_icon(GRID[(r, c)], os.path.join(ASSET, f"{symbol}-win.png"))

        push_icon(KEY_WIN, render_win_image(winnings))

        # Jackpot: all 9 same symbol — extra celebration
        flat = [grid[r][c] for r in range(3) for c in range(3)]
        if len(set(flat)) == 1:
            for cycle in range(8):
                for key in GRID.values():
                    symbol_path = os.path.join(ASSET, f"{flat[0]}.png")
                    push_icon(key, render_glitter_overlay(symbol_path, cycle + 50))
                push_icon(KEY_WIN, render_big_win_frame(winnings, cycle + 20))
                time.sleep(0.15)
    else:
        push_icon(KEY_WIN, render_win_image(0))

    # Update state
    state["grid"] = grid
    state["spinning"] = False
    save_state(state)

    # Show credits and re-enable SPIN
    push_icon(KEY_CREDITS, render_credits_image(state["credits"]))


def cmd_init():
    """Initialize the slot machine display."""
    state = load_state()
    if state.get("credits", 0) <= 0:
        state["credits"] = 100
        state["bet"] = 5
    show_ui(state)

    # Set initial grid
    if state.get("grid"):
        show_grid(state["grid"])
    else:
        grid = [[pick_symbol() for _ in range(3)] for _ in range(3)]
        state["grid"] = grid
        save_state(state)
        show_grid(grid)


def cmd_spin():
    """Handle SPIN button press."""
    state = load_state()

    # If broke, reset
    if state.get("credits", 0) <= 0:
        state["credits"] = 100
        state["bet"] = 5
        state["spinning"] = False
        save_state(state)
        show_ui(state)
        return

    # Don't allow spinning while already spinning
    if state.get("spinning"):
        return

    if state["credits"] < state["bet"]:
        push_text(KEY_CREDITS, "LOW!", "#ff4444", "#1a0000", 14)
        time.sleep(1)
        push_icon(KEY_CREDITS, render_credits_image(state["credits"]))
        return

    state["spinning"] = True
    save_state(state)

    animate_spin(state)


def cmd_bet_up():
    """Increase bet."""
    state = load_state()
    bets = [1, 2, 5, 10, 25]
    current = state.get("bet", 5)
    idx = bets.index(current) if current in bets else 2
    idx = min(idx + 1, len(bets) - 1)
    state["bet"] = bets[idx]
    save_state(state)


def cmd_bet_down():
    """Decrease bet."""
    state = load_state()
    bets = [1, 2, 5, 10, 25]
    current = state.get("bet", 5)
    idx = bets.index(current) if current in bets else 2
    idx = max(idx - 1, 0)
    state["bet"] = bets[idx]
    save_state(state)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: slots-game.py {init|spin|bet-up|bet-down}")
        sys.exit(1)

    cmd = sys.argv[1]
    if cmd == "init":
        cmd_init()
    elif cmd == "spin":
        cmd_spin()
    elif cmd == "bet-up":
        cmd_bet_up()
    elif cmd == "bet-down":
        cmd_bet_down()
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)
