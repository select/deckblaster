# deckblaster

Linux Stream Deck setup — config-file driven, live-reloading, scriptable.

## Hardware

**Elgato Stream Deck MK.2** (15 keys, 5×3 grid) connected via USB on Linux.

## What's in Here

deckblaster is built on a fork of [muesli/deckmaster](https://github.com/muesli/deckmaster) — a lightweight, config-file-driven Stream Deck daemon for Linux. The fork adds a live HTTP API for pushing key updates at runtime, an `icon_command` widget field for dynamic per-key icon rendering, a file-watcher for instant config reload, and a plugin folder layout so each feature lives in its own self-contained directory with its deck config, scripts, and assets.

Plugins: [Slot Machine](#slot-machine) · [Polymarket](#polymarket) · [Home Assistant](#home-assistant) · [Mouse Highlighter](#mouse-highlighter) · [GitHub PRs](#github-prs) · [Jira](#jira) · [Docker](#docker) · [Ports](#ports) · [Stream Deck Recorder](#stream-deck-recorder) · [Calendar](#calendar) · [Calendar Day View](#calendar-day-view) · [Zoom](#zoom) · [Calculator](#calculator) · [Virtual Desktops](#virtual-desktops)


## Quick Start

```bash
# Build
cd deckmaster
go build ./...

# Run (manually)
./deckmaster --watch --api :9990 --deck decks/main.deck

# Or via systemd (auto-starts when /dev/streamdeck appears)
systemctl --user start deckblaster.path
systemctl --user status deckblaster.service
journalctl --user -u deckblaster.service -f
```

## Rebuilding After Code Changes

```bash
cd deckmaster
go build ./...
systemctl --user restart deckblaster.service
```

## Config-only Changes

Just save the `.deck` file — the `--watch` flag auto-reloads within 200 ms. No restart needed.

---

## Plugins

### Main

<img src="docs/screenshots/main.png" width="400"/> <img src="docs/screenshots/apps.png" width="400"/>

Home screen. Shows clock, RAM gauge, next calendar event countdown, and a virtual desktop switcher (bottom row). Navigation buttons reach all other plugins.

Secondary screen is the Apps navigation hub — quick-launch for Zoom, Calculator, and the screen recorder.

---

### Slot Machine

<img src="docs/screenshots/slots.png" width="400"/>

Fully animated slot machine with 7 symbols, 5 pay lines, bet cycling, win detection, glitter animations and a credits display. State persists across restarts; resets to 100 credits when broke.

**Language:** Python (`uv`)

---

### Polymarket

<img src="docs/screenshots/polymarket.png" width="400"/> <img src="docs/screenshots/polymarket-cats.png" width="400"/>

Browse trending prediction market bets. Shows YES/NO percentages for three markets per page with pagination, category filtering, and auto-refresh. Press a text key to scroll long titles.

**Language:** Python (`uv`) · **Requires:** internet access

---

### Home Assistant

<img src="docs/screenshots/ha.png" width="400"/>

Controls lights and switches in multiple rooms. Monitors door sensors (last opened / currently open) and soil moisture sensors. Room icons show live on/off state with a coloured dot.

**Language:** Python (`uv`) · **Requires:** `imagemagick`, HA token + URL in `~/.config/deckblaster.env`

---

### Mouse Highlighter

<img src="docs/screenshots/highlight.png" width="400"/>

Controls a cursor highlight circle (X11). Adjust radius, colour (uses pywal palette), and opacity live. Each parameter button shows a preview of the *next* value before you press it.

**Language:** Python (`uv`) · **Requires:** X11, bundled `highlight-pointer` binary (built from [swillner/highlight-pointer](https://github.com/swillner/highlight-pointer))

---

### GitHub PRs

<img src="docs/screenshots/github.png" width="400"/>

**Main-deck badge:**

<img src="docs/screenshots/github-badge.png" width="72"/>

Shows all your open pull requests across repos (up to 12). Each card displays the PR title, repo name, CI status, review state and comment count.

- **Top bar** — CI progress bar while running (yellow → orange when past average → red on failure); solid colour when resolved
- **Footer** — CI± label · average CI time (while running) or comment count with `mdi:message-outline` · review state (APR / WAIT / REQ / DRFT)
- **Header key** — official GitHub mark, open PR count, summary of failures/changes/running CIs
- **Main-deck badge** — 4 corner counts: approved (green, top-left) · waiting for review (yellow, top-right) · CI running (yellow, bottom-left) · problems (red, bottom-right)
- **Press any PR card** — opens the PR in the browser via `xdg-open`

Smart polling: 30 s while any CI is running, 1 min for 1 h after pressing a card, 30 min otherwise. CI average is sampled from the last 20 merged PRs via GraphQL and cached for 24 h in `/tmp`.

---

### Jira

<img src="docs/screenshots/jira.png" width="400"/>

**Main-deck badge:**

<img src="docs/screenshots/jira-badge.png" width="72"/>

Shows your open Jira issues from EN and AW boards in the current sprint (up to 12 per page). Each card displays the issue title, board + number, priority, type, and status.

- **Top bar** — colour-coded by status: blue (To Do), red (Blocked), yellow (In Progress), purple (In Review)
- **Footer** — priority dot (red/yellow/green) · issue type · short status label (TODO / BLKD / PROG / REVW)
- **Header key** — Jira logo, total issue count, status summary
- **Main-deck badge** — 4 corner counts: open (blue) · blocked (red) · in progress (yellow) · total (green)
- **Pagination** — cycles through pages when more than 12 issues; wraps around
- **Press any issue card** — opens the issue in the browser via `xdg-open`

Polling: 2-minute cache TTL. Filters out Done, Closed, Won’t Do, Duplicate, and Epics. Only shows issues from open sprints.

**Language:** TypeScript (Bun) + sharp · **Requires:** `JIRA_URL`, `JIRA_USERNAME`, `JIRA_API_TOKEN` in `~/.config/deckblaster.env`

---

### Docker

<img src="docs/screenshots/docker.png" width="400"/>

Shows all containers (running/stopped/paused) with image name, mapped port and uptime. Press a container to start/stop it. Paginates when you have more than 9 containers.

**Language:** Bun / JS · **Requires:** `bun`, `docker`

---

### Ports

<img src="docs/screenshots/ports.png" width="400"/>

Lists all listening dev-server ports — filters out system processes and shows port, process name, project directory and framework (Next.js, Vite, Django, …). No external package needed; scanner is inlined.

**Language:** Bun / JS · **Requires:** `bun`, `ss` (iproute2)

---

### Stream Deck Recorder

<img src="docs/screenshots/recorder.png" width="400"/>

Single button on the Apps page. Records the Stream Deck's own button display at 5 fps and encodes to `~/Videos/` on stop. Each frame is composited onto the Stream Deck photo template.

**Language:** Python (stdlib only)

---

### Calendar

<img src="docs/screenshots/calendar-widget.png" width="100"/>

Next calendar event from GNOME Evolution Data Server (Outlook/O365) on the main deck. Colour-coded countdown, event title, −3/−2/−1 min alert flashes, press to open meeting URL.

**Language:** Python (stdlib + `python3-gi`) · **Requires:** `python3-gi`, `gir1.2-ecal-2.0`, `gir1.2-edataserver-1.2`

### Calendar Day View

<img src="docs/screenshots/calendar.png" width="400"/>

Day-view calendar with 9 event slots (3×3 grid), paginated by day. Events come from GNOME Evolution Data Server (Outlook/O365 via Evolution).

- **Colour-coded time bars** — 🟥 now · 🟧 ≤20 min · 🟨 ≤1 h · 🟦 ≤4 h · 🟩 >4 h · grey = past
- **Day navigation** — PREV / NEXT buttons to page through days; header shows day label, date and event count
- **Open URL** — press an event to open its meeting link (`xdg-open`)
- **Background fetcher** — daemon polls GNOME EDS every 5 min, pre-fetches 7 days (today ±3); renderer only reads JSON, never blocks on EDS
- **Deduplication** — filters duplicate events from shared / personal calendar copies
- **Outlook timezones** — handles Windows-style TZID values (e.g. `W. Europe Standard Time`) that libical silently misreads as UTC

**Language:** Bun/JS (sharp for SVG→PNG) + Python fetcher (GObject/EDS) · **Requires:** `bun`, `python3-gi`, `gir1.2-ecal-2.0`, `gir1.2-edataserver-1.2`

---

### Virtual Desktops

<img src="docs/screenshots/vdesktop.png" width="400"/>

Bottom row of the main deck. Each button shows the apps open on that virtual desktop using GTK icon theme lookup. The active desktop gets a blue underline. Press to switch. Icons are re-rendered every 3 seconds by a background poller.

Supports **X11** (xdotool) and **Wayland** (bundled GNOME Shell extension). Session type is auto-detected. On Wayland, if the extension isn't installed yet, the first desktop key shows a ⚠ INSTALL icon — press it to run the installer (requires one logout/login to activate).

**Language:** Python (stdlib + `python3-gi`) · **Requires:** `python3-gi`, `gir1.2-gtk-3.0`, `xdotool` (X11) or bundled extension (Wayland)

---

### Zoom

<img src="docs/screenshots/zoom.png" width="400"/>

Meeting controls: mute/unmute, video on/off, raise/lower hand, leave. All buttons show grey no-meeting icons when Zoom is not running and switch to coloured icons when a meeting is active.

**Language:** Python (stdlib only) · **Requires:** `xdotool`

---

### Calculator

<img src="docs/screenshots/calc.png" width="400"/>

LCD-style calculator. Supports chained operations, 12-digit input, division-by-zero error handling. Long-press `+` for `−`, `×` for `÷`, `=` for clear.

**Language:** Python (`uv`)

---


## Documentation

- [Solution assessment & project comparison](docs/assessment.md)
- [Extracting icons from Elgato plugins](docs/plugin-icons.md)
- [Prior art & research links](docs/prior-art.md)

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

| Feature | File | Description |
|---|---|---|
| `--watch` flag | `main.go` | `fsnotify`-based auto-reload on any `.deck` file change, including plugin subdirectories |
| `--api :PORT` flag | `main.go` | HTTP API: push live label / icon / background-color updates to individual keys at runtime |
| `icon` config field | `widget_command.go` | Static icon path on a `command` widget — renders a PNG instead of (or alongside) text |
| `icon_command` config field | `widget_command.go` | Dynamic icon: runs a shell command on each interval; the command prints a PNG path which is rendered as the key image. This is how all live-updating plugin buttons work (Docker badge, HA light state, ports count, …) |
| `color_command` config field | `widget_command.go` | Runs a shell command to determine text colour dynamically (used by the calendar countdown) |
| Solid-color background fill | `main.go` | HTTP API `/key/<n>` accepts a `background` hex field to flood-fill the key before rendering text |
| Plugin subdir path resolution | `config.go` | `parent =` and `deck =` references now resolve relative to each deck file's own directory, enabling the one-folder-per-plugin layout |
| Error resilience | `deck.go` | Widget update errors are logged to stderr instead of killing the process |
