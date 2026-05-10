import GLib from 'gi://GLib';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const OUT_PATH = '/tmp/streamdeck-wm.json';

export default class DeckblasterWM extends Extension {
    _timer = null;

    enable() {
        this._write();
        this._timer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 2, () => {
            this._write();
            return GLib.SOURCE_CONTINUE;
        });
    }

    disable() {
        if (this._timer) {
            GLib.source_remove(this._timer);
            this._timer = null;
        }
    }

    _write() {
        try {
            const wm = global.workspace_manager;
            const n = wm.get_n_workspaces();
            const active = wm.get_active_workspace_index();
            const desktops = {};

            for (let i = 0; i < n; i++) {
                const ws = wm.get_workspace_by_index(i);
                const wins = ws.list_windows().filter(w =>
                    !w.is_skip_taskbar() && !w.minimized
                );
                const apps = wins.map(w => {
                    const cls = w.get_wm_class();
                    return cls ? cls.toLowerCase() : '';
                }).filter(c => c);
                // Deduplicate
                desktops[i] = [...new Set(apps)];
            }

            const data = JSON.stringify({active, n, desktops});
            GLib.file_set_contents(OUT_PATH, data);
        } catch (e) {
            logError(e, 'deckblaster-wm');
        }
        return true;
    }
}
