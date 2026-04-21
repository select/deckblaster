# deckmaster (fork)

This is a fork of [muesli/deckmaster](https://github.com/muesli/deckmaster) with
the following additions:

| Feature | Description |
|---|---|
| `--watch` flag | `fsnotify`-based auto-reload on any `.deck` file change (including plugin subdirs) |
| `--api :PORT` flag | HTTP API: push live label/icon/background updates to individual keys |
| `icon` field | Static icon path in `command` widget config |
| `icon_command` field | Dynamic icon: runs a shell command that returns a PNG path |
| Error resilience | Widget update errors log to stderr instead of killing the process |
| Solid-color background | HTTP API `/key/<n>` accepts `background` hex field to fill key before rendering text |
| Plugin subdir path resolution | `parent =` and `deck =` fields resolve relative to each deck file's own directory, enabling the plugin folder layout |

For the original deckmaster documentation, see the
[upstream README](https://github.com/muesli/deckmaster/blob/master/README.md).

## Build

```bash
go build ./...
```

## Usage

```bash
./deckmaster --deck decks/main.deck --watch --api :9990 --brightness 60
```

See [`start.sh`](start.sh) for the full startup wrapper used by systemd.
