# Extracting Icons from Elgato Stream Deck Plugins on Linux

## Background

The official Elgato Stream Deck software is Windows/Mac only. Plugins from the
[Elgato Marketplace](https://marketplace.elgato.com) are distributed as
`.streamDeckPlugin` files which are **plain ZIP archives** containing PNGs, JS,
and a `manifest.json`. We can download and unzip them directly without the
official software.

## The Archive CDN

[OpenDeck](https://github.com/nekename/OpenDeck) (a Linux-native Stream Deck
app that supports original Elgato plugins) exposes the download mechanism in its
source:

```
# src/components/PluginManager.svelte
https://plugins.amankhanna.me/rezipped/<plugin-id>.zip
```

This CDN mirrors the old **Elgato App Store** (now replaced by the Marketplace).
The catalogue of all available plugins is at:

```
https://plugins.amankhanna.me/catalogue.json
```

Each entry has the plugin ID, name, author, version, and original Elgato CDN URL:

```json
{
  "id": "com.lostdomain.zoom",
  "name": "Zoom Plugin",
  "author": "Martijn Smit",
  "version": "3.0",
  "download": "https://appstore.elgato.com/streamDeckPlugin/com.lostdomain.zoom/3.0/com.lostdomain.zoom.streamDeckPlugin"
}
```

## How to Extract Icons from Any Plugin

```bash
# 1. Find the plugin ID in the catalogue
curl -s https://plugins.amankhanna.me/catalogue.json | python3 -m json.tool | grep -i "<name>" -A3

# 2. Download and unzip
mkdir -p /tmp/plugin-extract
curl -L "https://plugins.amankhanna.me/rezipped/<plugin-id>.zip" -o /tmp/plugin-extract/plugin.zip
unzip /tmp/plugin-extract/plugin.zip -d /tmp/plugin-extract/

# 3. Find the PNGs — @2x variants are 144x144 (higher quality)
find /tmp/plugin-extract -name "*@2x.png" | sort
```

## Zoom Plugin (`com.lostdomain.zoom`)

Downloaded from:
```
https://plugins.amankhanna.me/rezipped/com.lostdomain.zoom.zip
```

Icons extracted to `deckmaster/decks/assets/zoom/`:

| File | Source |
|---|---|
| `mic-on.png` | `streamdeck-zoom-unmuted@2x.png` |
| `mic-off.png` | `streamdeck-zoom-muted@2x.png` |
| `video-on.png` | `streamdeck-zoom-video-started@2x.png` |
| `video-off.png` | `streamdeck-zoom-video-stopped@2x.png` |
| `leave.png` | `streamdeck-zoom-leave@2x.png` |
| `no-meeting-mic.png` | `streamdeck-zoom-muted-closed@2x.png` |
| `no-meeting-video.png` | `streamdeck-zoom-video-closed@2x.png` |
| `no-meeting-leave.png` | `streamdeck-zoom-leave-closed@2x.png` |
| `share-start.png` | `streamdeck-zoom-share-start@2x.png` |
| `share-stop.png` | `streamdeck-zoom-share-stop@2x.png` |
| `no-meeting-share.png` | `streamdeck-zoom-share-closed@2x.png` |

The `-closed` variants are used for the "not in meeting" state — the plugin
uses them to indicate Zoom is not running.

## Notes

- The `amankhanna.me` CDN only covers the **old Elgato App Store** (pre-2023).
  Newer Marketplace plugins (like the official Elgato Zoom plugin v1.0.2) are
  not archived there and require the official software to download.
- `.streamDeckPlugin` files from any source are just ZIPs — `unzip` works on
  them directly even without renaming.
- The icons are copyrighted by their respective plugin authors. Use for personal
  local tooling only.
