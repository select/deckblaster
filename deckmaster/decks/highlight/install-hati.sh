#!/bin/bash
# Install the patched hati GNOME Shell extension for cursor highlighting.
# This version includes:
#   - GNOME 46+ support (metadata patched)
#   - Outer ring respects color alpha (allows fully transparent ring for click-only mode)
#
# After install, log out and back in for GNOME Shell to load the extension.

set -euo pipefail

EXTENSION_UUID="hati@szymonwilczek.github.io"
EXTENSION_DIR="$HOME/.local/share/gnome-shell/extensions/$EXTENSION_UUID"
SOURCE_DIR="$(dirname "$(readlink -f "$0")")/hati-extension"

if [ ! -d "$SOURCE_DIR" ]; then
    echo "❌ Source not found: $SOURCE_DIR"
    exit 1
fi

echo "📦 Installing patched hati extension..."
mkdir -p "$EXTENSION_DIR"
cp -r "$SOURCE_DIR"/* "$EXTENSION_DIR"/
glib-compile-schemas "$EXTENSION_DIR/schemas/"

echo "✅ Extension installed at $EXTENSION_DIR"
echo ""

# Enable the extension (may fail if GNOME Shell hasn't picked it up yet)
if gnome-extensions enable "$EXTENSION_UUID" 2>/dev/null; then
    echo "✅ Extension enabled."
else
    echo "⚠️  Could not enable yet — log out and back in, then run:"
    echo "   gnome-extensions enable $EXTENSION_UUID"
fi

echo ""
echo "🔄 Log out and back in for changes to take effect."
