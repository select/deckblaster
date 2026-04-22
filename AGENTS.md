# AGENTS.md — Stream Deck Setup

This document describes the full setup of the Stream Deck integration on this machine.
It is intended for AI agents and humans picking up this project.

---

## Hardware

- **Device**: Elgato Stream Deck MK.2 (15 buttons, 5×3 grid, serial `A00SA4122IRDY3`)
- **udev rule**: `/etc/udev/rules.d/99-streamdeck.rules` — creates `/dev/streamdeck` symlink, grants access to `plugdev` group

---

## Software Stack

| Component | Location |
|---|---|
| **deckmaster** (forked) | `deckmaster/` |
| **start.sh** (wrapper) | `deckmaster/start.sh` |
| Deck configs + scripts + assets | `deckmaster/decks/<plugin>/` |
| Shared assets (back, empty, …) | `deckmaster/decks/assets/` |
| Systemd units | `~/.config/systemd/user/` |
| Environment | `~/.config/deckblaster.env` |}

### deckmaster modifications (vs upstream)

- **`main.go`**: Added `--watch` flag (fsnotify auto-reload on `.deck` file save), `--api` flag (HTTP API for live key updates), extracted `reloadDeck()` helper; HTTP API `/key/<n>` accepts `background` field (hex color) to fill key with a solid color before rendering text
- **`widget_command.go`**: Added `icon` (static) and `icon_command` (dynamic, runs a shell command that returns a PNG path) config options; when no text commands are present the icon renders full-size
- **`deck.go`**: Widget update errors now log to stderr instead of killing the process

---

## Systemd

### Units

```
~/.config/systemd/user/deckblaster.path     — triggers service when /dev/streamdeck appears
~/.config/systemd/user/deckblaster.service  — runs start.sh (deckmaster + subprocesses in one cgroup)
```

`start.sh` launches subprocesses (vdesktop poller, calendar alert) in the background then
`exec`s deckmaster as the foreground process systemd tracks. All processes share one cgroup;
systemd kills them all together on stop.

### Managing the service

```bash
systemctl --user status deckblaster.service   # check status + process tree
systemctl --user restart deckblaster.service  # restart everything
systemctl --user stop deckblaster.service     # stop everything
journalctl --user -u deckblaster.service -f   # live logs
```

### Enabling autostart (already done)

```bash
systemctl --user enable deckblaster.path
```

### Environment file

`~/.config/deckblaster.env` contains:
```
HA_TOKEN=<long-lived Home Assistant access token>
```

---

## Deck Layout (`decks/test.deck`)

### Main page (15 keys, 5×3)

| Index | Widget | Description |
|---|---|---|
| 0 | `button` | → navigates to `polymarket.deck` |
| 1 | `button` | → navigates to `highlight.deck` |
| 2 | `button` | → navigates to `ha.deck` (lights page) |
| 3 | `button` | → navigates to `apps.deck` |
| 4 | `time` | Clock — `HH:mm` bold / `DD Mon` regular |
| 5 | `top` | Memory usage gauge (purple) |
| 6 | `command` | GitHub PR badge (live) — → navigates to `github.deck` |
| 7 | `command` | Docker badge — → navigates to `docker.deck` |
| 8 | `command` | Ports badge — → navigates to `ports.deck` |
| 9 | `command` | Next calendar event — countdown + title, refreshes every 30s |
| 10–14 | `command` | Virtual desktop row (bottom row) — app icons, refreshes every 3s |

### HA lights page (`decks/ha.deck`)

Parent: `test.deck` (back button returns here)

| Index | Room | Entity |
|---|---|---|
| 0 | Büro (Arbeitszimmer) | `switch.blaues_zimmer_schrank` |
| 1 | Wohnzimmer | `light.klavier` + others (via `ha-wohnzimmer-toggle.sh`) |
| 2 | Schlafzimmer | `light.schlafzimmer_decke` |
| 14 | Back | navigates to `test.deck` |

Each button uses `icon_command` to show a room icon with a green (ON) or red (OFF) dot, polling HA every 5s.

### GitHub PRs page (`decks/github/github.deck`)

Parent: `main.deck` (back button returns here)

| Index | Widget | Description |
|---|---|---|
| 0 | `command` | Header — GitHub logo, open PR count, CI/review summary |
| 1–12 | `command` | PR cards (PRs 0–11) — title, repo, CI status, comment count, review state |
| 13 | empty | |
| 14 | Back | navigates to `main.deck` |

Each PR card shows:
- **Top colour strip**: green (CI pass) / red (CI fail) / yellow (CI pending) / grey (no CI)
- **Repo name** (short) + **PR number**
- **Title** (up to 3 lines)
- **Status bar**: CI+/CI!/CI~ label · comment count (if > 0) · review state (APR/REQ/WAIT/DRFT)

**Review states**: APR = approved (green) · REQ = changes requested — *you need to respond* (red) · WAIT = waiting for review (yellow) · DRFT = draft (grey)

**Smart polling TTL** — deck interval is always 30 s, but the GitHub API is only called:
- every **30 s** when any PR has pending/running CI
- every **30 min** when all CIs are resolved

All slot images are pre-rendered in one atomic batch when the cache expires (serialised via lock file).

**Script**: `github/github-prs.py` — uses `gh api graphql` for a single cross-repo query
**State/cache**: `/tmp/streamdeck-github-prs.json` · images in `/tmp/streamdeck-github/`
**Lock**: `/tmp/streamdeck-github.lock`

### Slot Machine page (`decks/slots.deck`)

Parent: `test.deck` (exit button returns here)

| Index | Widget | Description |
|---|---|---|
| 0 | `command` | Credits display — also triggers `slots-game.py init` on deck load |
| 1,2,3 | `command` | Reel top row (driven by HTTP API) |
| 4 | `button` | Bet Up (cycles 1→2→5→10→25) |
| 5 | `button` | SPIN — runs `slots-game.py spin` |
| 6,7,8 | `command` | Reel middle row |
| 9 | `button` | Bet Down |
| 10 | `button` | EXIT → navigates to `test.deck` |
| 11,12,13 | `command` | Reel bottom row |
| 14 | `button` | Lines display (static: 5 lines) |

**Symbols**: cherry, lemon, bell, diamond, seven, star, bar — generated by `slots-generate-assets.py`.
**Animation**: Reels spin with colorful blur frames, columns stop left→middle→right with dramatic slowdown.
**Win detection**: 5 lines (3 horizontal + 2 diagonal). 3-of-a-kind pays full, 2-of-a-kind pays ⅓.
**Win animation**: Glitter particles overlay winning symbols with golden pulsing border + animated WIN display.
**State**: `/tmp/streamdeck-slots.json` — persists credits, bet, grid. Resets to 100 credits when broke.

### Mouse Highlighter page (`decks/highlight.deck`)

Parent: `test.deck` (exit button returns here)

| Index | Key | Action |
|---|---|---|
| 0 | Status | API-driven display (ON/OFF, preview circle, radius) |
| 1 | SIZE ▲ | Increase radius — preview shows next size |
| 2 | ◀ COLOR | Previous color — preview shows previous color swatch |
| 3 | ALPHA ▲ | Increase opacity — preview shows next opacity level |
| 4 | (empty) | |
| 5 | ON/OFF | Toggle highlight-pointer process |
| 6 | SIZE ▼ | Decrease radius — preview shows next smaller size |
| 7 | COLOR ▶ | Next color — preview shows next color swatch |
| 8 | ALPHA ▼ | Decrease opacity — preview shows next lower opacity |
| 9 | (empty) | |
| 10 | EXIT | → navigates to `test.deck` |
| 11–14 | (empty) | |

All 6 parameter buttons use `icon_command` polling (2s) via `highlight-btn-icon.py` to show
live previews of the *next* value — circle size, color swatch, or opacity level — using the
current highlight color.

**Binary**: `scripts/highlight-pointer` (built from [swillner/highlight-pointer](https://github.com/swillner/highlight-pointer), X11-only)
**Controller**: `scripts/highlight-ctrl.py` — manages process lifecycle, renders status display
**Button icons**: `scripts/highlight-btn-icon.py` — renders dynamic preview icons per button type
**State**: `/tmp/streamdeck-highlight.json`
**PID file**: `/tmp/streamdeck-highlight.pid`

### Calculator page (`decks/calc.deck`)

Parent: `test.deck` (exit button returns here)

| Index | Key | Action | Long-press |
|---|---|---|---|
| 0 | Display | LCD-style readout (API-driven) | — |
| 1 | 7 | digit 7 | — |
| 2 | 8 | digit 8 | — |
| 3 | 9 | digit 9 | — |
| 4 | + | add | subtract (−) |
| 5 | 0 | digit 0 | — |
| 6 | 4 | digit 4 | — |
| 7 | 5 | digit 5 | — |
| 8 | 6 | digit 6 | — |
| 9 | × | multiply | divide (÷) |
| 10 | EXIT | → navigates to `test.deck` | — |
| 11 | 1 | digit 1 | — |
| 12 | 2 | digit 2 | — |
| 13 | 3 | digit 3 | — |
| 14 | = | equals | clear (C) |

**Display**: Green-on-black LCD style, right-aligned, auto-sizing font. Shows operator indicator at top-left during chained operations.
**Features**: Chained operations (3×4+2=14), division-by-zero error handling, 12-digit input.
**State**: `/tmp/streamdeck-calc.json`

### Zoom page (`decks/zoom.deck`)

Parent: `test.deck` (back button returns here)

| Index | Action | Shortcut |
|---|---|---|
| 0 | Mute / Unmute | `Alt+A` |
| 1 | Start / Stop video | `Alt+V` |
| 2 | Raise / Lower hand | `Alt+Y` |
| 4 | Leave meeting | `Alt+Q` |
| 14 | Back | navigates to `test.deck` |

Buttons use `icon_command` polling every 3s. Icons are green/red/grey based on local state file
`/tmp/streamdeck-zoom-state`. State resets automatically when the Zoom meeting window disappears.
No meeting → all buttons show grey icon.

---

## Plugin Layout (`decks/<plugin>/`)

Each plugin lives in its own folder under `deckmaster/decks/` containing the `.deck` config, all
its scripts, and an `assets/` subdirectory for its icons. Shared icons (back, empty) are in
`deckmaster/decks/assets/`.

| Plugin folder | Contents |
|---|---|
| `ha/` | `ha.deck`, HA toggle/indicator/icon/door/moisture scripts, room assets |
| `zoom/` | `zoom.deck`, mic/video/hand/leave icon + action scripts, zoom icon assets |
| `slots/` | `slots.deck`, `slots-game.py`, bet/spin/generate scripts, all slot symbol assets |
| `github/` | `github.deck`, `github-prs.py`, assets/ |
| `calc/` | `calc.deck`, `calc-game.py`, `calc-generate-assets.py`, digit/op icon assets |
| `highlight/` | `highlight.deck`, `highlight-ctrl.py`, `highlight-btn-icon.py`, binary, `wal_colors.py`, assets |
| `polymarket/` | `polymarket.deck`, `polymarket-cats.deck`, all polymarket scripts, assets |
| `docker/` | `docker.deck`, `docker-render.js`, action/icon shell scripts, assets |
| `ports/` | `ports.deck`, `ports-render.js`, page/icon shell scripts, assets |
| `vdesktop/` | `vdesktop-render.py`, `vdesktop-icon.sh`, `vdesktop-switch.sh`, `vdesktop-poll.sh`, `assets/app-*.png` |
| `calendar/` | `next-event.py`, countdown/title/color/open shell scripts |
| `recorder/` | `deck-record.py`, `deck-record-toggle.py`, icon script, recording assets |

### Key scripts

| Script | Plugin | Purpose |
|---|---|---|
| `calendar/next-event.py` | Calendar | Fetches next calendar event from GNOME EDS; caches 2 min; fires HTTP API alert at −3/−2/−1 min |
| `ha/ha-light-toggle.sh <entity>` | HA | Toggles a HA light entity via REST API |
| `ha/ha-switch-toggle.sh <entity>` | HA | Toggles a HA switch entity via REST API |
| `ha/ha-door-poll.sh` | HA | Background daemon: pushes 20s red alert when any door opens |
| `ha/ha-moisture-icon.sh` | HA | Renders soil moisture level icon (orange/green/blue) |
| `zoom/zoom-send-key.sh <key>` | Zoom | Focuses Zoom window, sends keystroke, restores focus |
| `vdesktop/vdesktop-render.py` | VDesktop | Renders desktop button images to `/tmp/streamdeck-vdesktop/desk-N.png` |
| `vdesktop/vdesktop-poll.sh` | VDesktop | Background daemon: re-renders all desktop images every 3s |
| `calc/calc-game.py` | Calc | Calculator engine — digit/op/equals/clear, pushes display to API key 0 |
| `highlight/highlight-ctrl.py` | Highlight | Controller — toggle/size/color/opacity, manages `highlight-pointer` process |
| `highlight/highlight-btn-icon.py` | Highlight | Renders live preview icons per button type (size/color/alpha) |
| `highlight/highlight-pointer` | Highlight | Compiled binary — X11 cursor highlight overlay |
| `slots/slots-game.py` | Slots | Slot machine engine — spin/bet/win logic, pushes reel images via API |
| `docker/docker-render.js` | Docker | Renders container status keys (Bun/sharp); called by docker-*.sh |
| `ports/ports-render.js` | Ports | Renders listening-port keys (Bun/sharp); called by ports-*.sh |

---

## HTTP API

Running on `:9990` when deckmaster is started with `--api :9990`.

```bash
# Push a temporary notification to a key (auto-reverts after duration)
curl -X POST localhost:9990/key/0 \
  -d '{"label":"Alert","color":"#ff0000","fontsize":12,"duration":"10s"}'

# Trigger config reload
curl -X POST localhost:9990/reload

# Health check
curl localhost:9990/health
```

---

## Home Assistant

- **URL**: set via `HA_URL` in `~/.config/deckblaster.env`
- **Auth**: Bearer token in `~/.config/deckblaster.env` (`HA_TOKEN`)
- Room → entity mapping:

| Room | Entity | Domain |
|---|---|---|
| Büro / Blaues Zimmer | `switch.blaues_zimmer_schrank` | switch |
| Wohnzimmer | `light.klavier`, `light.tv_lampe`, `light.esstisch`, `light.fenster_wohnzimmer` | light |
| Schlafzimmer | `light.schlafzimmer_decke` | light |

---

## Calendar Integration

- Source: GNOME Evolution Data Server (EDS) — syncs Outlook/O365 calendar (`Apheris` source)
- Python lib: `gir1.2-ecal-2.0` + `gir1.2-edataserver-1.2`
- Cache TTL: 2 minutes (`/tmp/streamdeck-next-event.json`)
- Countdown format: `15m` / `2h05m` / `1d3h` / `now`

---

## Virtual Desktops

- Desktop environment: GNOME on X11 (`DISPLAY=:1`)
- Tool: `xdotool` — `get_num_desktops`, `get_desktop`, `get_desktop_for_window`, `set_desktop`
- Icon resolution priority:
  1. Manual overrides (chromium snap, zed local, firefox/kitty system paths)
  2. GTK icon theme via `gir1.2-gtk-3.0` + `.desktop` file lookup using 4 keys per app: `StartupWMClass`, file stem, `Exec` basename, `Name` — builds 507-entry map
  3. Substring / token fallback across the full map
  4. Direct GTK theme lookup by WM_CLASS as icon name
  5. Downloaded fallback icons in `vdesktop/assets/app-*.png`
- Icon cache: `/tmp/streamdeck-icon-cache/` — invalidated if source icon mtime is newer
- Rendered images: `/tmp/streamdeck-vdesktop/desk-N.png`
- Poller runs as `streamdeck-vdesktop.service`, bound to `deckblaster.service` (stops/starts together)

---

## Important Rules for New Deck Pages

1. **Every `.deck` file must define all 15 keys (indices 0–14).** Unused keys must be set to `../assets/empty.png` (for plugin decks in subdirs) or `assets/empty.png` (for `main.deck`/`apps.deck`). Deckmaster does not clear keys when switching decks — leftover icons from the previous page will remain visible if a key is not explicitly set.

2. **Never push to action keys via the HTTP API.** The API replaces the widget and destroys its action/action_hold bindings. Only push to display-only keys (no `[keys.action]` block). Use static `icon` in the `.deck` config for buttons with actions.

---

## Rebuilding After Code Changes

```bash
cd /path/to/streamdeck/deckmaster
go build ./...
systemctl --user restart deckblaster.service
```

## Config-only Changes

Just save the `.deck` file — the `--watch` flag auto-reloads within 200ms. No restart needed.
