#!/usr/bin/env bash
# scripts/setup-assets.sh — generate all static button assets
#
# Run once after cloning (or after wiping assets/) to produce the PNG files
# that are pre-rendered at build time rather than at runtime.
#
# Requirements: uv, bun, go (to build deckmaster)
# Usage: bash scripts/setup-assets.sh

set -euo pipefail
cd "$(dirname "$0")/.."          # run from repo root
DECKS="deckmaster/decks"

echo "▶ Building deckmaster…"
(cd deckmaster && go build ./...)
echo "  ✓ deckmaster built"

echo ""
echo "▶ Generating calc assets…"
uv run "$DECKS/calc/calc-generate-assets.py"
echo "  ✓ calc/assets/"

echo ""
echo "▶ Generating slots assets…"
uv run "$DECKS/slots/slots-generate-assets.py"
uv run "$DECKS/slots/slots-generate-icon.py"
echo "  ✓ slots/assets/"

echo ""
echo "▶ Generating highlight assets…"
uv run "$DECKS/highlight/highlight-generate-assets.py"
echo "  ✓ highlight/assets/"

echo ""
echo "▶ Generating polymarket assets…"
uv run "$DECKS/polymarket/polymarket-generate-assets.py"
echo "  ✓ polymarket/assets/"

echo ""
echo "▶ Generating docker nav assets…"
bun "$DECKS/docker/docker-render.js" generate-assets
echo "  ✓ docker/assets/"

echo ""
echo "▶ Generating ports nav assets (bun install first if needed)…"
(cd "$DECKS" && bun install --frozen-lockfile 2>/dev/null || bun install)
echo "  ✓ node_modules ready"

echo ""
echo "✅ All assets generated."
echo ""
echo "Next steps:"
echo "  1. Copy .env.example → ~/.config/streamdeck.env and fill in your values"
echo "  2. systemctl --user enable --now streamdeck.path"
