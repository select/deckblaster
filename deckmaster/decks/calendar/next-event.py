#!/usr/bin/env python3
"""Fetch next calendar event from GNOME EDS.

Outputs countdown or title for the deckmaster command widget.
On each call also checks alert thresholds and fires HTTP API pushes
if within 3 / 2 / 1 minute of the event.

Usage:
  next-event.py countdown   → "2h05m" (to start) or "-15m" (to end, during event)
  next-event.py title       → "Engine Team…"
  next-event.py color       → "#00ffcc;#ffffff" or "#5599ff;#ffffff" (during event)

State file: /tmp/streamdeck-next-event-alerts.json
  Tracks which thresholds have been fired per event so duplicate
  calls (countdown + title poll within same 30s window) don't double-fire.
"""
import sys
import os
import re
import json
import time
import subprocess
import urllib.request
import gi
gi.require_version('ECal', '2.0')
gi.require_version('EDataServer', '1.2')
gi.require_version('ICalGLib', '3.0')
from gi.repository import ECal, EDataServer, ICalGLib
from datetime import datetime, timezone, timedelta
from zoneinfo import ZoneInfo

# ── config ────────────────────────────────────────────────────────────────────
LOOKAHEAD_DAYS = 7
CACHE_FILE      = "/tmp/streamdeck-next-event.json"
ALERT_FILE      = "/tmp/streamdeck-next-event-alerts.json"
CACHE_TTL       = 120   # seconds before re-querying EDS
API_KEY         = 9     # stream deck key index for the calendar button
API_URL         = f"http://localhost:9990/key/{API_KEY}"

# threshold_seconds → (flash_duration, label, background_color)
ALERTS = {
    180: ("5s",  "3 MIN",  "#cc0000"),
    120: ("5s",  "2 MIN",  "#cc0000"),
    60:  ("20s", "1 MIN!", "#cc0000"),
}

# ── EDS fetch ─────────────────────────────────────────────────────────────────

# Outlook/Exchange uses Windows timezone names; map the common ones to IANA.
WINDOWS_TZ = {
    "W. Europe Standard Time":       "Europe/Berlin",
    "Central Europe Standard Time":  "Europe/Budapest",
    "Romance Standard Time":         "Europe/Paris",
    "GMT Standard Time":             "Europe/London",
    "Eastern Standard Time":         "America/New_York",
    "Central Standard Time":         "America/Chicago",
    "Mountain Standard Time":        "America/Denver",
    "Pacific Standard Time":         "America/Los_Angeles",
    "UTC":                           "UTC",
}

def ical_time_to_dt(tt, tzid=None):
    """Convert ICalGLib.Time to a UTC-aware datetime.

    libical does not recognise Windows-style TZID values and silently treats
    them as UTC, causing events to appear hours late.  When a tzid is supplied
    we resolve it through WINDOWS_TZ (or pass it straight to zoneinfo for IANA
    names) so the local wall-clock time is interpreted correctly.
    """
    naive = datetime(
        tt.get_year(), tt.get_month(), tt.get_day(),
        tt.get_hour(), tt.get_minute(), tt.get_second(),
    )
    if tzid:
        iana = WINDOWS_TZ.get(tzid, tzid)
        try:
            return naive.replace(tzinfo=ZoneInfo(iana)).astimezone(timezone.utc)
        except Exception:
            pass  # unknown TZ name — fall through to UTC
    if tt.is_utc():
        return naive.replace(tzinfo=timezone.utc)
    # Floating time: assume system local timezone
    return naive.astimezone().astimezone(timezone.utc)

def fetch_next_event():
    registry = EDataServer.SourceRegistry.new_sync(None)
    sources  = registry.list_sources(EDataServer.SOURCE_EXTENSION_CALENDAR)
    now      = datetime.now(timezone.utc)
    end      = now + timedelta(days=LOOKAHEAD_DAYS)
    now_iso  = now.strftime("%Y%m%dT%H%M%SZ")
    end_iso  = end.strftime("%Y%m%dT%H%M%SZ")
    query    = f'(occur-in-time-range? (make-time "{now_iso}") (make-time "{end_iso}"))'
    events   = []

    for source in sources:
        try:
            client = ECal.Client.connect_sync(
                source, ECal.ClientSourceType.EVENTS, 10, None)
            ok, comps = client.get_object_list_as_comps_sync(query, None)
            if not ok:
                continue
            for comp in comps:
                # Read the raw TZID from the base DTSTART *before* EDS normalises
                # it.  Outlook uses Windows-style names like "W. Europe Standard
                # Time" that libical can't resolve, so it silently treats them as
                # UTC.  We stash the original tzid in a closure so instance_cb
                # can do the conversion correctly.
                ical = comp.get_icalcomponent()
                prop = ical.get_first_property(ICalGLib.PropertyKind.DTSTART_PROPERTY)
                raw_tzid = None
                if prop:
                    param = prop.get_first_parameter(ICalGLib.ParameterKind.TZID_PARAMETER)
                    raw_tzid = param.get_tzid() if param else None

                # Also get raw TZID for DTEND
                end_prop = ical.get_first_property(ICalGLib.PropertyKind.DTEND_PROPERTY)
                raw_end_tzid = None
                if end_prop:
                    end_param = end_prop.get_first_parameter(ICalGLib.ParameterKind.TZID_PARAMETER)
                    raw_end_tzid = end_param.get_tzid() if end_param else None

                def instance_cb(ical_comp, instance_start, instance_end, _cancel, _ud,
                                tzid=raw_tzid, end_tzid=raw_end_tzid):
                    is_allday = instance_start.is_date()
                    dt_start = ical_time_to_dt(instance_start, tzid=tzid)
                    dt_end   = ical_time_to_dt(instance_end, tzid=end_tzid)
                    summary  = ical_comp.get_summary() or "?"
                    location = ical_comp.get_location() or ""
                    if dt_start <= now < dt_end:
                        # Skip all-day events as in-progress — treat them as future
                        if not is_allday:
                            events.append((dt_start.isoformat(), summary, dt_end.isoformat(), True, location))
                    elif dt_start > now:
                        # Future event (all-day events included here)
                        events.append((dt_start.isoformat(), summary, dt_end.isoformat(), False, location))
                    return True

                client.generate_instances_for_object_sync(
                    ical,
                    int(now.timestamp()), int(end.timestamp()),
                    None, instance_cb, None)
        except Exception:
            continue

    if not events:
        return None
    # Prioritise in-progress events, then sort by start time
    in_progress = [e for e in events if e[3]]
    future      = [e for e in events if not e[3]]
    in_progress.sort(key=lambda x: x[0])
    future.sort(key=lambda x: x[0])
    pick = in_progress[0] if in_progress else future[0]
    return {
        "dt": pick[0],
        "summary": pick[1],
        "end_dt": pick[2],
        "in_progress": pick[3],
        "location": pick[4] if len(pick) > 4 else "",
        "fetched": time.time(),
    }

def get_cached_event():
    try:
        if os.path.exists(CACHE_FILE):
            with open(CACHE_FILE) as f:
                data = json.load(f)
            if time.time() - data.get("fetched", 0) < CACHE_TTL:
                return data
    except Exception:
        pass
    data = fetch_next_event()
    if data:
        try:
            with open(CACHE_FILE, "w") as f:
                json.dump(data, f)
        except Exception:
            pass
    return data

# ── alerts ────────────────────────────────────────────────────────────────────

def load_alert_state():
    try:
        with open(ALERT_FILE) as f:
            return json.load(f)
    except Exception:
        return {}

def save_alert_state(state):
    try:
        with open(ALERT_FILE, "w") as f:
            json.dump(state, f)
    except Exception:
        pass

def push_alert(label, duration, bg):
    payload = json.dumps({
        "label":      label,
        "color":      "#ffffff",
        "background": bg,
        "fontsize":   18,
        "duration":   duration,
    }).encode()
    try:
        req = urllib.request.Request(
            API_URL, data=payload,
            headers={"Content-Type": "application/json"}, method="POST")
        urllib.request.urlopen(req, timeout=2)
    except Exception:
        pass

def check_alerts(event_dt_iso, remaining_secs):
    state = load_alert_state()

    # Reset fired list when the event changes
    if state.get("dt") != event_dt_iso:
        state = {"dt": event_dt_iso, "fired": []}

    fired = state.get("fired", [])
    changed = False

    for threshold, (duration, label, bg) in ALERTS.items():
        if threshold not in fired and 0 < remaining_secs <= threshold:
            push_alert(label, duration, bg)
            fired.append(threshold)
            changed = True

    if changed:
        state["fired"] = fired
        save_alert_state(state)

# ── formatting ────────────────────────────────────────────────────────────────

def format_countdown(delta):
    total_min = int(delta.total_seconds() // 60)
    if total_min < 1:
        return "now"
    h, m = divmod(total_min, 60)
    if h >= 24:
        return f"{h // 24}d"
    if h > 0:
        return f"{h}h"
    return f"{m}m"

def truncate(text, max_len=12):
    return text if len(text) <= max_len else text[:max_len - 1] + "…"

# ── main ──────────────────────────────────────────────────────────────────────

def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "countdown"
    data = get_cached_event()

    if not data:
        print("-")
        return

    dt          = datetime.fromisoformat(data["dt"])
    end_dt      = datetime.fromisoformat(data["end_dt"]) if data.get("end_dt") else None
    in_progress = data.get("in_progress", False)
    now         = datetime.now(timezone.utc)
    delta       = dt - now
    remaining   = delta.total_seconds()

    # Always check alerts on every call (deduped via state file)
    if not in_progress:
        check_alerts(data["dt"], remaining)

    if mode == "countdown":
        if in_progress and end_dt:
            # Count down to end of meeting
            end_delta = end_dt - now
            print(format_countdown(end_delta))
        else:
            print(format_countdown(delta))
    elif mode == "title":
        print(truncate(data["summary"]))
    elif mode == "color":
        if in_progress:
            print("#5599ff;#ffffff")  # blue countdown during event
        else:
            print("#ffffff;#ffffff")  # white when counting down to future event
    elif mode == "open":
        loc = data.get("location", "")
        m = re.search(r'https?://\S+', loc)
        if m:
            subprocess.Popen(["xdg-open", m.group(0)],
                             env={**os.environ, "DISPLAY": os.environ.get("DISPLAY", ":1")})

if __name__ == "__main__":
    main()
