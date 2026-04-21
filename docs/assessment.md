# Stream Deck Linux Solutions Assessment

## Your Requirements

1. **Config-file driven** (no mandatory UI)
2. **Live config updates** while running (hot-reload)
3. **Live notifications on the deck** (push dynamic content to keys)
4. **Windows config import** (low priority)

---

## Project Comparison

| Project | Language | Stars | Last Commit | Active? |
|---|---|---|---|---|
| **deckmaster** | Go | 291 | 2024-05 | ⚠️ Stale (1yr+) |
| muesli/streamdeck | Go (lib) | 66 | 2023-04 | ❌ Stale (3yr) |
| dh1tw/streamdeck | Go (lib) | 86 | 2025-09 | ✅ Maintained |
| **OpenDeck** | Rust/TS | 1531 | 2026-04-13 | ✅ Very active |
| **StreamController** | Python | 992 | 2026-04-13 | ✅ Very active |
| streamdeck-ui | Python | 1268 | 2024-01 | ❌ Stale (2yr+) |
| streamdeck-tricks | Go | — | 2025-04 | ⚠️ Personal project |

---

## Detailed Assessment

### 🥇 deckmaster (muesli) — **Best fit for your needs**

**Pros:**
- ✅ **Pure config-file driven** — TOML `.deck` files, no UI at all
- ✅ **SIGHUP hot-reload** — `kill -HUP <pid>` reloads config live, validated before swap
- ✅ **Command widget** — runs shell commands on interval and displays output on keys (perfect for live notifications)
- ✅ **Widget system** — time, CPU/mem, weather, command output, buttons with icons+text
- ✅ **Multi-page decks** with navigation
- ✅ **Short & long press** actions
- ✅ Simple Go binary, easy to extend
- ✅ Exec, keycode, clipboard, D-Bus actions

**Cons:**
- ⚠️ Last commit May 2024 — not dead but stale
- ⚠️ No Windows config import
- ⚠️ No file-watcher (must send SIGHUP manually, but trivially fixable with `inotifywait` or code patch)
- ⚠️ Uses deprecated `ioutil`, older Go patterns
- ⚠️ X11-only for recent-window widget (Wayland gap)

**Live notifications approach:** The `command` widget already polls shell commands at configurable intervals and renders output on keys. You can:
1. Point commands at scripts that check notification sources
2. Use SIGHUP to swap entire deck configs for alert states
3. Add an `fsnotify` watcher (small patch) for automatic reload on file change

---

### 🥈 OpenDeck — **Most feature-complete, but UI-centric**

**Pros:**
- ✅ Very actively maintained (commit yesterday)
- ✅ Supports **original Elgato Stream Deck plugins** (huge ecosystem)
- ✅ **Windows config import** — uses Elgato-compatible profile format, profiles portable across platforms
- ✅ Cross-platform (Linux, macOS, Windows)
- ✅ Multi-action, toggle actions, auto profile switching
- ✅ Rust backend (fast, safe)

**Cons:**
- ❌ **UI-first design** (Tauri/Svelte frontend) — not config-file driven
- ❌ No documented CLI/API for live key updates from scripts
- ❌ Heavy dependencies (Tauri, Deno, Wine for Windows plugins)
- ⚠️ Config is in its own format, not a simple editable file

**Verdict:** Best if you want the Windows plugin ecosystem. Overkill and wrong paradigm for config-file + live-update workflow.

---

### 🥉 StreamController — **Plugin-rich but heavy**

**Pros:**
- ✅ Very actively maintained
- ✅ Plugin system with store
- ✅ Auto page switching per active window
- ✅ Python — easy to script/extend

**Cons:**
- ❌ **UI-first** (GTK app, Flatpak distribution)
- ❌ High memory usage (acknowledged in README)
- ❌ No config-file workflow
- ❌ No Windows config import
- ⚠️ Beta quality

**Verdict:** Good for GUI users, wrong fit for your requirements.

---

### Others

- **streamdeck-ui**: Stale (2yr), Python/Qt UI. Has import/export but UI-mandatory. Dead project.
- **dh1tw/streamdeck**: Pure Go library, well-maintained. Good as building block if writing from scratch, but no app/config system.
- **muesli/streamdeck**: Low-level Go library that deckmaster uses. Stale.
- **streamdeck-tricks**: Personal project by lornajane, not general-purpose. Interesting as reference for Go-based custom setups.

---

## Recommendation

### Primary: **deckmaster** + enhancements

Deckmaster is the clear winner for your workflow. It's the only project that is:
- Config-file native (TOML)
- Already has SIGHUP live reload
- Has a command widget for live data display
- Simple Go codebase (~800 lines) that's easy to fork and extend

### Suggested enhancements to build:

1. **`fsnotify` file watcher** — auto-reload on `.deck` file change (replaces manual SIGHUP)
2. **Notification widget** — watches a file/pipe/socket for messages, displays them on a key with timeout
3. **HTTP/Unix socket API** — push updates to specific keys at runtime (e.g., `curl localhost:9999/key/3 -d '{"icon":"alert.png","label":"3 new msgs"}'`)
4. **Windows config importer** (low prio) — parse Elgato's JSON profiles and generate `.deck` TOML files

### Alternative: Build on dh1tw/streamdeck library

If deckmaster proves too stale to fork comfortably, dh1tw's library is well-maintained and you could build a lean custom daemon with:
- TOML/YAML config
- fsnotify watcher
- HTTP API for live updates
- Notification rendering

This is more work but gives you full control.

---

## Next Steps

1. Build & test deckmaster with your Stream Deck hardware
2. Add `fsnotify` watcher for auto-reload
3. Add a notification widget or HTTP API for live key updates
4. (Later) Write a Windows config importer
