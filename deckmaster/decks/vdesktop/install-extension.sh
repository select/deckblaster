#!/bin/bash
# Install the deckblaster-wm GNOME Shell extension for Wayland workspace tracking.
# This copies extension files and enables the extension.
# A session restart (log out/in) is required for GNOME Shell to load it.

set -euo pipefail

EXT_UUID="deckblaster-wm@local"
EXT_DIR="${HOME}/.local/share/gnome-shell/extensions/${EXT_UUID}"
SRC_DIR="$(dirname "$0")/gnome-extension"

if [ ! -f "${SRC_DIR}/extension.js" ]; then
    echo "ERROR: extension source not found at ${SRC_DIR}" >&2
    exit 1
fi

echo "Installing ${EXT_UUID}..."
mkdir -p "${EXT_DIR}"
cp "${SRC_DIR}/extension.js" "${EXT_DIR}/"
cp "${SRC_DIR}/metadata.json" "${EXT_DIR}/"

# Enable the extension via gsettings
CURRENT=$(gsettings get org.gnome.shell enabled-extensions 2>/dev/null || echo "@as []")
if echo "$CURRENT" | grep -q "${EXT_UUID}"; then
    echo "Extension already in enabled-extensions list."
else
    # Add to the array
    if [ "$CURRENT" = "@as []" ]; then
        NEW="['${EXT_UUID}']"
    else
        NEW=$(echo "$CURRENT" | sed "s/]$/, '${EXT_UUID}']/" )
    fi
    gsettings set org.gnome.shell enabled-extensions "$NEW" 2>/dev/null
    echo "Added to enabled-extensions."
fi

echo "Installed to ${EXT_DIR}"
echo ""
echo ">>> Log out and back in for the extension to take effect. <<<"
