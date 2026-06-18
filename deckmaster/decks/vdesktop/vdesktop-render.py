#!/usr/bin/env python3
"""Generate Stream Deck buttons showing virtual desktop app icons.

- Resolves icons from system theme / .desktop files / pixmaps
- Icons are as large as possible given the number of apps
- No text labels — icons only
- Active desktop has a thin blue underline
- Renders to /tmp/streamdeck-vdesktop/desk-N.png
"""
import os
import glob
import subprocess
import json
import re
import shutil
import time
import tempfile
from pathlib import Path

DISPLAY = os.environ.get("DISPLAY", ":0")
if not DISPLAY:
    DISPLAY = ":0"
    os.environ["DISPLAY"] = DISPLAY

# Wayland support: auto-detect XAUTHORITY for XWayland if not already set
if not os.environ.get("XAUTHORITY"):
    import glob as _glob
    _xauth = _glob.glob(f"/run/user/{os.getuid()}/.mutter-Xwaylandauth.*")
    if _xauth:
        os.environ["XAUTHORITY"] = _xauth[0]

ASSETS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "assets")
OUT_DIR = "/tmp/streamdeck-vdesktop"
ICON_CACHE_DIR = "/tmp/streamdeck-icon-cache"
MAX_APPS = 5
BUTTON_SIZE = 72

os.makedirs(OUT_DIR, exist_ok=True)
os.makedirs(ICON_CACHE_DIR, exist_ok=True)
# Pass DISPLAY through from environment — set in .env or inherited from user session


def run(cmd, timeout=5):
    try:
        return subprocess.check_output(
            cmd, shell=True, stderr=subprocess.DEVNULL, timeout=timeout
        ).decode().strip()
    except Exception:
        return ""


# ---------------------------------------------------------------------------
# Icon resolution
# ---------------------------------------------------------------------------

def build_icon_map():
    """Build a map from WM_CLASS (lowercase) -> icon file path.
    Indexes by StartupWMClass, desktop file stem, Exec basename, and Name.
    """
    icon_map = {}

    # Manual overrides for known tricky apps
    manual = {
        "chromium":        "/snap/chromium/current/chromium.png",
        "dev.zed.zed":     os.path.expanduser("~/.local/zed.app/share/icons/hicolor/512x512/apps/zed.png"),
        "firefox_firefox": "/usr/share/icons/hicolor/128x128/apps/firefox.png",
        "navigator":       "/usr/share/icons/hicolor/128x128/apps/firefox.png",
        "kitty":           "/usr/share/icons/hicolor/256x256/apps/kitty.png",
    }
    for k, v in manual.items():
        if os.path.exists(v):
            icon_map[k] = v

    try:
        import gi
        gi.require_version("Gtk", "3.0")
        from gi.repository import Gtk
        theme = Gtk.IconTheme.get_default()

        def resolve_icon(icon_name):
            if not icon_name:
                return None
            if icon_name.startswith("/") and os.path.exists(icon_name):
                return icon_name
            for size in [64, 128, 48, 256, 32]:
                info = theme.lookup_icon(icon_name, size, 0)
                if info:
                    return info.get_filename()
            return None

        desktop_dirs = [
            "/usr/share/applications",
            os.path.expanduser("~/.local/share/applications"),
            "/var/lib/flatpak/exports/share/applications",
            "/var/lib/snapd/desktop/applications",
        ]

        for d in desktop_dirs:
            for f in glob.glob(d + "/*.desktop"):
                try:
                    lines = open(f, errors="ignore").read().splitlines()
                    props = {}
                    in_main = True
                    for line in lines:
                        if line.startswith("[") and line != "[Desktop Entry]":
                            in_main = False
                        if in_main and "=" in line:
                            k, v = line.split("=", 1)
                            props[k.strip()] = v.strip()

                    icon_path = resolve_icon(props.get("Icon", ""))
                    if not icon_path:
                        continue

                    # Collect all possible lookup keys for this app
                    keys = set()

                    # 1. StartupWMClass (most precise)
                    wmc = props.get("StartupWMClass", "").lower().strip()
                    if wmc:
                        keys.add(wmc)

                    # 2. Desktop file stem (covers "org.gnome.Nautilus", "firefox", etc.)
                    stem = Path(f).stem.lower()
                    keys.add(stem)
                    keys.add(stem.split(".")[-1])  # last dotted component

                    # 3. Exec basename
                    exec_val = props.get("Exec", "")
                    if exec_val:
                        exec_bin = exec_val.split()[0].split("/")[-1].lower()
                        exec_bin = re.sub(r"[^a-z0-9]", "", exec_bin)
                        if exec_bin:
                            keys.add(exec_bin)

                    # 4. App Name
                    name = props.get("Name", "").lower().strip()
                    if name:
                        keys.add(name)
                        keys.add(re.sub(r"[^a-z0-9]", "", name))

                    for key in keys:
                        if key and key not in icon_map:
                            icon_map[key] = icon_path

                except Exception:
                    continue
    except Exception:
        pass

    # Pixmaps fallback
    for f in glob.glob("/usr/share/pixmaps/*.png") + glob.glob("/usr/share/pixmaps/*.svg"):
        name = Path(f).stem.lower()
        if name not in icon_map:
            icon_map[name] = f

    return icon_map


_icon_map = None

def get_icon_path(wm_class):
    global _icon_map
    if _icon_map is None:
        _icon_map = build_icon_map()

    cls = wm_class.lower().strip()

    # 1. Exact match
    if cls in _icon_map:
        return _icon_map[cls]

    # 2. Substring match
    for key, path in _icon_map.items():
        if len(key) >= 3 and (key in cls or cls in key):
            return path

    # 3. Token match (e.g. "org.gnome.nautilus" -> "nautilus")
    for part in re.split(r"[.\-_]", cls):
        if len(part) >= 3 and part in _icon_map:
            return _icon_map[part]

    # 4. Direct GTK theme lookup as last resort
    try:
        import gi
        gi.require_version("Gtk", "3.0")
        from gi.repository import Gtk
        theme = Gtk.IconTheme.get_default()
        for candidate in [cls, cls.split(".")[-1], cls.replace("_", "-")]:
            info = theme.lookup_icon(candidate, 64, 0)
            if info:
                return info.get_filename()
    except Exception:
        pass

    return f"{ASSETS}/app-default.png"


def cached_icon(wm_class, size):
    """Return path to a size-converted cached icon PNG, trimmed to fill."""
    raw = get_icon_path(wm_class)
    if not raw or not os.path.exists(raw):
        return f"{ASSETS}/app-default.png"

    cache_key = re.sub(r"[^a-zA-Z0-9]", "_", wm_class) + f"_{size}.png"
    cache_path = f"{ICON_CACHE_DIR}/{cache_key}"

    # Regenerate if source icon is newer than cache
    if not os.path.exists(cache_path) or os.path.getmtime(raw) > os.path.getmtime(cache_path):
        run(
            f"convert -background none '{raw}' "
            f"-trim +repage "
            f"-resize {size}x{size} "
            f"-background none -gravity center -extent {size}x{size} "
            f"PNG32:'{cache_path}'"
        )

    return cache_path if os.path.exists(cache_path) else f"{ASSETS}/app-default.png"


# ---------------------------------------------------------------------------
# Window Manager & Session detection
# ---------------------------------------------------------------------------

WM_JSON = "/tmp/streamdeck-wm.json"
INSTALL_ICON = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets", "install-ext.png")

_wm_detected = None


def get_window_manager():
    """Detect the current window manager.
    Returns: 'hyprland', 'gnome_wayland', or 'x11'.
    """
    global _wm_detected
    if _wm_detected is not None:
        return _wm_detected

    # 1. Check if HYPRLAND_INSTANCE_SIGNATURE is set and working
    if os.environ.get("HYPRLAND_INSTANCE_SIGNATURE"):
        try:
            out = subprocess.check_output(
                "hyprctl activeworkspace -j", shell=True, stderr=subprocess.DEVNULL, timeout=2
            )
            if out:
                _wm_detected = "hyprland"
                return "hyprland"
        except Exception:
            pass

    # 2. Try to auto-discover active Hyprland signature
    uid = os.getuid()
    hypr_dir = f"/run/user/{uid}/hypr"
    if os.path.exists(hypr_dir):
        for entry in os.scandir(hypr_dir):
            if entry.is_dir():
                lock_path = os.path.join(entry.path, "hyprland.lock")
                if os.path.exists(lock_path):
                    try:
                        with open(lock_path) as f:
                            pid_str = f.readline().strip()
                        if pid_str.isdigit():
                            pid = int(pid_str)
                            if os.path.exists(f"/proc/{pid}"):
                                with open(f"/proc/{pid}/comm") as cf:
                                    comm = cf.read().strip()
                                if "Hyprland" in comm:
                                    os.environ["HYPRLAND_INSTANCE_SIGNATURE"] = entry.name
                                    out = subprocess.check_output(
                                        "hyprctl activeworkspace -j", shell=True, stderr=subprocess.DEVNULL, timeout=2
                                    )
                                    if out:
                                        _wm_detected = "hyprland"
                                        return "hyprland"
                    except Exception:
                        pass

    # 3. Check GNOME Wayland
    desktop = os.environ.get("XDG_CURRENT_DESKTOP", "").lower()
    session_type = os.environ.get("XDG_SESSION_TYPE", "").lower()
    if "gnome" in desktop and session_type == "wayland":
        _wm_detected = "gnome_wayland"
        return "gnome_wayland"

    # 4. Fallback to X11
    _wm_detected = "x11"
    return "x11"


def check_extension_needed():
    """Return True if extension is needed but missing/not running."""
    wm = get_window_manager()
    if wm == "gnome_wayland":
        try:
            age = time.time() - os.path.getmtime(WM_JSON)
            return age >= 10
        except OSError:
            return True
    return False


# ---------------------------------------------------------------------------
# Window / desktop info
# ---------------------------------------------------------------------------

def get_windows_hyprland():
    """Read workspace/window data from hyprctl clients -j."""
    desktops = {}
    try:
        out = run("hyprctl clients -j")
        if out:
            clients = json.loads(out)
            for client in clients:
                if not client.get("mapped", False):
                    continue
                ws_id = client.get("workspace", {}).get("id")
                if ws_id is None:
                    continue
                # Map 1-based workspace ID to 0-based index
                desk = ws_id - 1
                # Standard display is 5 slots, so only process desks 0..4
                if 0 <= desk < 5:
                    cls = client.get("class", "").lower().strip()
                    if not cls:
                        continue
                    if desk not in desktops:
                        desktops[desk] = []
                    if cls not in desktops[desk]:
                        desktops[desk].append(cls)
    except Exception:
        pass
    return desktops


def get_current_desktop_hyprland():
    """Get 0-indexed active workspace ID on Hyprland."""
    try:
        out = run("hyprctl activeworkspace -j")
        if out:
            data = json.loads(out)
            # Map 1-based workspace ID to 0-based index
            return int(data.get("id", 1)) - 1
    except Exception:
        pass
    return 0


def get_num_desktops_hyprland():
    """Return number of desktops on Hyprland. Since the Stream Deck always
    displays 5 workspace slots (indices 0 to 4), we always return 5.
    """
    return 5


def get_windows_wayland():
    """Read workspace/window data from GNOME Shell extension JSON."""
    try:
        with open(WM_JSON) as f:
            data = json.load(f)
        desktops = {}
        for k, apps in data.get("desktops", {}).items():
            desktops[int(k)] = apps
        return desktops
    except Exception:
        return {}


def get_current_desktop_wayland():
    try:
        with open(WM_JSON) as f:
            data = json.load(f)
        return data.get("active", 0)
    except Exception:
        return 0


def get_num_desktops_wayland():
    try:
        with open(WM_JSON) as f:
            data = json.load(f)
        return data.get("n", 0)
    except Exception:
        return 0


def get_windows_x11():
    desktops = {}
    wids = run("xdotool search --onlyvisible --name ''").split("\n")
    for wid in wids:
        wid = wid.strip()
        if not wid:
            continue
        desk = run(f"xdotool get_desktop_for_window {wid}")
        if not desk or not desk.isdigit():
            continue
        desk = int(desk)
        wm_class_raw = run(f"xprop -id {wid} WM_CLASS")
        m = re.search(r'"([^"]*)",\s*"([^"]*)"', wm_class_raw)
        if not m:
            continue
        cls = m.group(2).lower()
        if desk not in desktops:
            desktops[desk] = []
        if cls not in desktops[desk]:
            desktops[desk].append(cls)
    return desktops


def get_current_desktop_x11():
    d = run("xdotool get_desktop")
    return int(d) if d.isdigit() else 0


def get_num_desktops_x11():
    n_str = run("xdotool get_num_desktops")
    return int(n_str) if n_str.isdigit() else 0


# Unified interface
def get_windows():
    wm = get_window_manager()
    if wm == "hyprland":
        return get_windows_hyprland()
    elif wm == "gnome_wayland":
        return get_windows_wayland()
    else:
        return get_windows_x11()


def get_current_desktop():
    wm = get_window_manager()
    if wm == "hyprland":
        return get_current_desktop_hyprland()
    elif wm == "gnome_wayland":
        return get_current_desktop_wayland()
    else:
        return get_current_desktop_x11()


def get_num_desktops():
    wm = get_window_manager()
    if wm == "hyprland":
        return get_num_desktops_hyprland()
    elif wm == "gnome_wayland":
        return get_num_desktops_wayland()
    else:
        return get_num_desktops_x11()


# ---------------------------------------------------------------------------
# Rendering
# ---------------------------------------------------------------------------

def render_desktop(desk_num, apps, current):
    out_path = f"{OUT_DIR}/desk-{desk_num}.png"
    n = min(len(apps), MAX_APPS)

    if n == 0:
        # Empty desktop: gray square
        col = "#3a3a3a"
        cmd = f"convert -size {BUTTON_SIZE}x{BUTTON_SIZE} xc:'{col}' "
        if current:
            cmd += f"-fill '#18bcf2' -draw 'rectangle 0,{BUTTON_SIZE - 4} {BUTTON_SIZE},{BUTTON_SIZE}' "
        cmd += f"PNG32:'{out_path}'"
        run(cmd)
        return out_path

    PAD = max(1, int(BUTTON_SIZE * 0.05))  # 5% = ~4px
    available = BUTTON_SIZE - 2 * PAD

    icon_size = available
    gap = 2

    if n > 1:
        icon_size = (available - (n - 1) * gap) // n

    total_w = n * icon_size + (n - 1) * gap
    start_x = (BUTTON_SIZE - total_w) // 2
    start_y = (BUTTON_SIZE - icon_size) // 2

    cmd = f"convert -size {BUTTON_SIZE}x{BUTTON_SIZE} xc:'#1a1a1a' "

    for i, cls in enumerate(apps[:MAX_APPS]):
        icon = cached_icon(cls, icon_size)
        x = start_x + i * (icon_size + gap)
        cmd += f"\\( '{icon}' -resize {icon_size}x{icon_size} \\) -geometry +{x}+{start_y} -composite "

    # Active indicator overlaid on top
    if current:
        cmd += f"-fill '#18bcf2' -draw 'rectangle 0,{BUTTON_SIZE - 4} {BUTTON_SIZE},{BUTTON_SIZE}' "

    cmd += f"PNG32:'{out_path}'"
    run(cmd)
    return out_path


def main():
    import sys
    cmd = sys.argv[1] if len(sys.argv) > 1 else "render"

    if cmd == "switch":
        # Switch to virtual desktop N
        n = sys.argv[2] if len(sys.argv) > 2 else "0"
        wm = get_window_manager()
        if wm == "gnome_wayland" and check_extension_needed():
            # Extension not installed — run installer
            install_script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "install-extension.sh")
            subprocess.run(["bash", install_script])
            return
        if wm == "hyprland":
            # Hyprland workspaces are 1-based, switch to workspace n+1
            workspace_num = int(n) + 1
            run(f"hyprctl dispatch 'hl.dsp.focus({{workspace={workspace_num}}})'")
        elif wm == "gnome_wayland":
            # Use gdbus to switch workspace on GNOME Wayland
            run(f"gdbus call --session --dest org.gnome.Shell "
                f"--object-path /org/gnome/Shell/Extensions/DeckblasterWM "
                f"--method org.gnome.Shell.Extensions.DeckblasterWM.SwitchWorkspace {n}")
        else:
            subprocess.run(["xdotool", "set_desktop", n])
        return

    if cmd == "poll":
        # Daemon loop: re-render every 3 seconds (replaces vdesktop-poll.sh)
        import time
        while True:
            try:
                _render_all()
            except Exception:
                pass
            time.sleep(3)
        return

    # Default: render once
    _render_all()


def _render_all():
    # On GNOME Wayland, show install icon if extension is not running
    if check_extension_needed():
        # Copy install icon to desk-0 slot so it shows on the deck
        out_path = f"{OUT_DIR}/desk-0.png"
        shutil.copy2(INSTALL_ICON, out_path)
        # Clear other slots
        for i in range(1, 5):
            p = f"{OUT_DIR}/desk-{i}.png"
            if os.path.exists(p):
                os.remove(p)
        result = {0: {"image": out_path, "apps": [], "current": False}}
        print(json.dumps(result))
        return

    num_desktops = get_num_desktops()
    if num_desktops == 0:
        return

    current = get_current_desktop()
    windows = get_windows()

    result = {}
    for d in range(num_desktops):
        apps = windows.get(d, [])
        img = render_desktop(d, apps, d == current)
        result[d] = {"image": img, "apps": apps, "current": d == current}

    print(json.dumps(result))


if __name__ == "__main__":
    main()
