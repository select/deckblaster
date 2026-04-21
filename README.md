# Stream Deck Setup

Personal Stream Deck integration for Linux — config-file driven, live-reloading, scriptable.

## Hardware

**Elgato Stream Deck MK.2** (15 keys, 5×3 grid) connected via USB on Linux.

---

## What's in Here

| Directory | Purpose |
|---|---------|
| `deckmaster/` | Forked & extended Stream Deck daemon (the active solution) |
| `deckmaster/decks/<plugin>/` | Each plugin: `.deck` config + scripts + `assets/` |
| `docs/` | Research notes and design decisions |

---

## Chosen Solution: deckmaster

We evaluated **7 different Stream Deck Linux projects** before settling on a fork of
[muesli/deckmaster](https://github.com/muesli/deckmaster). See the full
[assessment and comparison →](docs/assessment.md).

### Why deckmaster?

Our hard requirements were:

1. **Config-file driven** — no mandatory UI
2. **Live config updates** — hot-reload while running
3. **Live key notifications** — push dynamic content to specific keys at runtime

deckmaster is the only project that meets all three:

- ✅ **Pure TOML config** — `.deck` files, no UI whatsoever
- ✅ **Auto file-watcher** — we added `fsnotify` so the deck reloads within 200 ms of any `.deck` file save (no `SIGHUP` needed)
- ✅ **HTTP API** — we added a `/key/<n>` endpoint so scripts can push live updates to individual keys (used for calendar alerts, slot machine animations, calculator display, mouse highlighter status)
- ✅ **Command widget** — runs shell commands on a configurable interval and renders output on keys; perfect for HA light states, virtual desktops, Zoom status
- ✅ **Small, hackable Go codebase** (~800 lines) — easy to fork and extend
- ✅ **Multi-page decks** with navigation, short/long press actions, icons + text

### Why not the others?

| Project | Reason rejected |
|---|---|
| [OpenDeck](https://github.com/nekename/OpenDeck) | UI-first (Tauri/Svelte app), no config-file workflow, heavy deps |
| [StreamController](https://github.com/StreamController/StreamController) | UI-first (GTK/Flatpak), no config-file workflow, high memory usage |
| [streamdeck-ui](https://github.com/timothycrosley/streamdeck-ui) | Stale (2 yr+), Python/Qt, UI-mandatory |
| [streamdeck-tricks](https://github.com/lornajane/streamdeck-tricks) | Personal project, not general-purpose |
| [dh1tw/streamdeck](https://github.com/dh1tw/streamdeck) | Raw Go library only — no config system, would need building from scratch |
| [muesli/streamdeck](https://github.com/muesli/streamdeck) | Low-level library (deckmaster's own dependency), stale |

---

## Our Additions to deckmaster

| Feature | Description |
|---|---|
| `--watch` flag | `fsnotify`-based auto-reload on `.deck` file change |
| `--api :9990` flag | HTTP API: push live updates to individual keys |
| `icon` / `icon_command` fields | Static and dynamic icon support in the command widget |
| Solid-color background fill | HTTP API accepts `background` hex color to fill a key before rendering text |
| Error resilience | Widget update errors log to stderr instead of killing the process |

---

## Quick Start

```bash
# Build
cd deckmaster
go build ./...

# Run (manually)
./deckmaster --watch --api :9990 --deck decks/test.deck

# Or via systemd (auto-starts when /dev/streamdeck appears)
systemctl --user start streamdeck.service
systemctl --user status streamdeck.service
journalctl --user -u streamdeck.service -f
```

## Rebuilding After Code Changes

```bash
cd deckmaster
go build ./...
systemctl --user restart streamdeck.service
```

## Config-only Changes

Just save the `.deck` file — the `--watch` flag auto-reloads within 200 ms. No restart needed.

---

## Deck Pages

| Page | File | Description |
|---|---|---|
| Main | `deckmaster/decks/main.deck` | Clock, memory gauge, calendar event, virtual desktops |
| HA Lights | `deckmaster/decks/ha/ha.deck` | Home Assistant room light toggles |
| Zoom | `deckmaster/decks/zoom/zoom.deck` | Mute, video, hand-raise, leave |
| Slot Machine | `deckmaster/decks/slots/slots.deck` | Fully animated slot machine game |
| Mouse Highlighter | `deckmaster/decks/highlight/highlight.deck` | Control X11 cursor highlight overlay |
| Calculator | `deckmaster/decks/calc/calc.deck` | LCD-style calculator with chained ops |
| Polymarket | `deckmaster/decks/polymarket/polymarket.deck` | Trending prediction market bets |
| Docker | `deckmaster/decks/docker/docker.deck` | Container status + start/stop |
| Ports | `deckmaster/decks/ports/ports.deck` | Listening dev-server ports |

## Screenshots

### Main
![Main deck](docs/screenshots/main.png)

### Home Assistant
![HA deck](docs/screenshots/ha.png)

### Zoom
![Zoom deck](docs/screenshots/zoom.png)

### Slot Machine
![Slots deck](docs/screenshots/slots.png)

### Calculator
![Calc deck](docs/screenshots/calc.png)

### Mouse Highlighter
![Highlight deck](docs/screenshots/highlight.png)

### Polymarket
![Polymarket deck](docs/screenshots/polymarket.png)

### Docker
![Docker deck](docs/screenshots/docker.png)

### Ports
![Ports deck](docs/screenshots/ports.png)

---

## Documentation

- [Solution assessment & project comparison](docs/assessment.md)
- [Extracting icons from Elgato plugins](docs/plugin-icons.md)
- [Prior art & research links](docs/prior-art.md)
- [Scripts README](scripts/README.md)
