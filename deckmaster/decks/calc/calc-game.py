#!/usr/bin/env python3
"""
Calculator for Stream Deck MK.2.

Uses the deckmaster HTTP API (:9990) to update the display key (0) only.
All button appearances are static from the .deck config — never API-pushed.

Layout (5×3):
  [DISP] [ 7 ] [ 8 ] [ 9 ] [ +/- ]
  [  0 ] [ 4 ] [ 5 ] [ 6 ] [ ×/÷ ]
  [EXIT] [ 1 ] [ 2 ] [ 3 ] [ =/C ]

State: /tmp/streamdeck-calc.json
"""

import json
import math
import os
import sys
import urllib.request
from PIL import Image, ImageDraw, ImageFont

API = "http://localhost:9990"
ASSET = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
STATE_FILE = "/tmp/streamdeck-calc.json"
TMP = "/tmp/streamdeck-calc"
os.makedirs(TMP, exist_ok=True)

SIZE = 72
KEY_DISPLAY = 0

try:
    FONT_XL = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 30)
    FONT_LG = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 22)
    FONT_MD = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 16)
    FONT_SM = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 11)
except Exception:
    FONT_XL = FONT_LG = FONT_MD = FONT_SM = ImageFont.load_default()


def load_state():
    if os.path.exists(STATE_FILE):
        with open(STATE_FILE) as f:
            return json.load(f)
    return default_state()


def default_state():
    return {
        "display": "0",
        "operand1": None,
        "operator": None,
        "new_input": True,
        "error": False,
    }


def save_state(state):
    with open(STATE_FILE, "w") as f:
        json.dump(state, f)


def push_icon(key, icon_path):
    data = json.dumps({"icon": icon_path}).encode()
    req = urllib.request.Request(f"{API}/key/{key}", data=data,
                                headers={"Content-Type": "application/json"})
    try:
        urllib.request.urlopen(req, timeout=2)
    except Exception as e:
        print(f"API error key {key}: {e}", file=sys.stderr)


def format_number(val):
    """Format a number for display — strip trailing zeros, handle large/small."""
    if val is None:
        return "0"
    if isinstance(val, str):
        return val

    # Handle special values
    if math.isinf(val):
        return "INF" if val > 0 else "-INF"
    if math.isnan(val):
        return "ERR"

    # Integer check
    if val == int(val) and abs(val) < 1e12:
        return str(int(val))

    # Format with reasonable precision
    formatted = f"{val:.8g}"
    return formatted


def render_display(state):
    """Render the calculator display to a 72×72 image."""
    img = Image.new("RGBA", (SIZE, SIZE), (0, 0, 0, 255))
    draw = ImageDraw.Draw(img)

    # Dark green LCD background
    for y in range(SIZE):
        t = y / SIZE
        r = int(10 + 8 * t)
        g = int(22 + 12 * t)
        b = int(10 + 8 * t)
        draw.line([(0, y), (SIZE - 1, y)], fill=(r, g, b))

    draw.rounded_rectangle([2, 2, 70, 70], radius=6, outline=(40, 80, 40), width=2)

    text = state.get("display", "0")
    error = state.get("error", False)
    color = (255, 60, 60) if error else (0, 255, 80)

    # Show operator indicator at top
    op = state.get("operator")
    if op and not state.get("new_input", True):
        op_symbol = {"+": "+", "-": "−", "*": "×", "/": "÷"}.get(op, op)
        draw.text((6, 4), op_symbol, fill=(100, 200, 100), font=FONT_SM)

    # Auto-size the display text
    if len(text) <= 4:
        font = FONT_XL
    elif len(text) <= 6:
        font = FONT_LG
    elif len(text) <= 9:
        font = FONT_MD
    else:
        font = FONT_SM
        text = text[:14]  # hard truncate

    bbox = draw.textbbox((0, 0), text, font=font)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]

    # Right-align the number
    x = SIZE - tw - 8
    y = (SIZE - th) // 2 + 4
    draw.text((x, y), text, fill=color, font=font)

    path = os.path.join(TMP, "display.png")
    img.save(path)
    return path


def update_display(state):
    path = render_display(state)
    push_icon(KEY_DISPLAY, path)


# ── Calculator operations ──────────────────────────────────────────────────────

def cmd_init():
    state = load_state()
    update_display(state)


def cmd_digit(d):
    state = load_state()
    state["error"] = False

    if state.get("new_input"):
        state["display"] = str(d)
        state["new_input"] = False
    else:
        current = state["display"]
        if current == "0":
            state["display"] = str(d)
        elif len(current) < 12:
            state["display"] = current + str(d)

    save_state(state)
    update_display(state)


def do_calculate(a, op, b):
    """Perform the arithmetic operation."""
    if op == "+":
        return a + b
    elif op == "-":
        return a - b
    elif op == "*":
        return a * b
    elif op == "/":
        if b == 0:
            return None  # division by zero
        return a / b
    return b


def cmd_operator(op):
    state = load_state()
    state["error"] = False

    current = float(state["display"])

    # Chain operations: if there's a pending operation, compute it first
    if state.get("operator") and not state.get("new_input"):
        prev = state["operand1"]
        if prev is not None:
            result = do_calculate(prev, state["operator"], current)
            if result is None:
                state = default_state()
                state["display"] = "ERR"
                state["error"] = True
                save_state(state)
                update_display(state)
                return
            state["display"] = format_number(result)
            current = result

    state["operand1"] = current
    state["operator"] = op
    state["new_input"] = True

    save_state(state)
    update_display(state)


def cmd_equals():
    state = load_state()

    if state.get("operator") and state.get("operand1") is not None:
        current = float(state["display"])
        result = do_calculate(state["operand1"], state["operator"], current)

        if result is None:
            state = default_state()
            state["display"] = "ERR"
            state["error"] = True
        else:
            state["display"] = format_number(result)
            state["operand1"] = None
            state["operator"] = None
            state["new_input"] = True
            state["error"] = False
    else:
        state["new_input"] = True

    save_state(state)
    update_display(state)


def cmd_clear():
    state = default_state()
    save_state(state)
    update_display(state)


# ── CLI ────────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: calc-game.py {init|digit <n>|op <+|-|*|/>|equals|clear}")
        sys.exit(1)

    cmd = sys.argv[1]

    if cmd == "init":
        cmd_init()
    elif cmd == "digit":
        cmd_digit(int(sys.argv[2]))
    elif cmd == "op":
        cmd_operator(sys.argv[2])
    elif cmd == "equals":
        cmd_equals()
    elif cmd == "clear":
        cmd_clear()
    else:
        print(f"Unknown: {cmd}", file=sys.stderr)
        sys.exit(1)
