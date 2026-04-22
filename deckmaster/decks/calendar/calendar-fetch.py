#!/usr/bin/env python3
"""Calendar event fetcher daemon for Stream Deck.

Runs in a loop, fetches 7 days of events from GNOME EDS every 5 minutes,
writes JSON to ~/.local/share/deckblaster/calendar-day-events.json.

Usage:
  calendar-fetch.py poll          → run as daemon (loop every 5 min)
  calendar-fetch.py once          → fetch once and exit
  calendar-fetch.py -3 -2 -1 ... → fetch specific day offsets, print JSON to stdout
"""
import sys
import json
import time
from pathlib import Path
from datetime import datetime, timezone, timedelta
from zoneinfo import ZoneInfo

import gi
gi.require_version('ECal', '2.0')
gi.require_version('EDataServer', '1.2')
gi.require_version('ICalGLib', '3.0')
from gi.repository import ECal, EDataServer, ICalGLib

CACHE_DIR = Path.home() / ".local" / "share" / "deckblaster"
EVENTS_FILE = CACHE_DIR / "calendar-day-events.json"
PREFETCH = range(-3, 4)  # 7 days
POLL_INTERVAL = 300  # 5 minutes

WINDOWS_TZ = {
    "W. Europe Standard Time": "Europe/Berlin",
    "Central Europe Standard Time": "Europe/Budapest",
    "Romance Standard Time": "Europe/Paris",
    "GMT Standard Time": "Europe/London",
    "Eastern Standard Time": "America/New_York",
    "Central Standard Time": "America/Chicago",
    "Mountain Standard Time": "America/Denver",
    "Pacific Standard Time": "America/Los_Angeles",
    "UTC": "UTC",
}


def ical_time_to_dt(tt, tzid=None):
    naive = datetime(
        tt.get_year(), tt.get_month(), tt.get_day(),
        tt.get_hour(), tt.get_minute(), tt.get_second(),
    )
    if tzid:
        iana = WINDOWS_TZ.get(tzid, tzid)
        try:
            return naive.replace(tzinfo=ZoneInfo(iana)).astimezone(timezone.utc)
        except Exception:
            pass
    if tt.is_utc():
        return naive.replace(tzinfo=timezone.utc)
    return naive.astimezone().astimezone(timezone.utc)


def connect_sources():
    registry = EDataServer.SourceRegistry.new_sync(None)
    sources = registry.list_sources(EDataServer.SOURCE_EXTENSION_CALENDAR)
    clients = []
    for source in sources:
        try:
            client = ECal.Client.connect_sync(source, ECal.ClientSourceType.EVENTS, 10, None)
            clients.append(client)
        except Exception:
            continue
    return clients


def fetch_day(clients, day_offset):
    local_tz = datetime.now().astimezone().tzinfo
    local_now = datetime.now().astimezone()
    target_date = (local_now + timedelta(days=day_offset)).date()
    day_start = datetime.combine(target_date, datetime.min.time()).replace(tzinfo=local_tz)
    day_end = day_start + timedelta(days=1)
    day_start_utc = day_start.astimezone(timezone.utc)
    day_end_utc = day_end.astimezone(timezone.utc)

    start_iso = day_start_utc.strftime("%Y%m%dT%H%M%SZ")
    end_iso = day_end_utc.strftime("%Y%m%dT%H%M%SZ")
    query = f'(occur-in-time-range? (make-time "{start_iso}") (make-time "{end_iso}"))'
    events = []

    for client in clients:
        try:
            ok, comps = client.get_object_list_as_comps_sync(query, None)
            if not ok:
                continue
            for comp in comps:
                ical = comp.get_icalcomponent()
                prop = ical.get_first_property(ICalGLib.PropertyKind.DTSTART_PROPERTY)
                raw_tzid = None
                if prop:
                    param = prop.get_first_parameter(ICalGLib.ParameterKind.TZID_PARAMETER)
                    raw_tzid = param.get_tzid() if param else None
                end_prop = ical.get_first_property(ICalGLib.PropertyKind.DTEND_PROPERTY)
                raw_end_tzid = None
                if end_prop:
                    end_param = end_prop.get_first_parameter(ICalGLib.ParameterKind.TZID_PARAMETER)
                    raw_end_tzid = end_param.get_tzid() if end_param else None

                def instance_cb(ical_comp, instance_start, instance_end, _cancel, _ud,
                                tzid=raw_tzid, end_tzid=raw_end_tzid):
                    if instance_start.is_date():
                        return True
                    dt_start = ical_time_to_dt(instance_start, tzid=tzid)
                    dt_end = ical_time_to_dt(instance_end, tzid=end_tzid)
                    events.append({
                        "dt": dt_start.isoformat(),
                        "end_dt": dt_end.isoformat(),
                        "summary": ical_comp.get_summary() or "?",
                        "location": ical_comp.get_location() or "",
                    })
                    return True

                client.generate_instances_for_object_sync(
                    ical,
                    int(day_start_utc.timestamp()), int(day_end_utc.timestamp()),
                    None, instance_cb, None)
        except Exception:
            continue
    events.sort(key=lambda e: e["dt"])
    # Deduplicate by start + end + summary
    seen = set()
    unique = []
    for e in events:
        key = (e["dt"], e["end_dt"], e["summary"])
        if key not in seen:
            seen.add(key)
            unique.append(e)
    return unique


def fetch_all_days():
    clients = connect_sources()
    result = {}
    for offset in PREFETCH:
        result[str(offset)] = fetch_day(clients, offset)
    return result


def write_events(days):
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    data = {"days": days, "fetchedAt": int(time.time() * 1000)}
    with open(EVENTS_FILE, "w") as f:
        json.dump(data, f)


def main():
    args = sys.argv[1:]
    cmd = args[0] if args else "once"

    if cmd == "poll":
        print(f"Calendar fetcher started, polling every {POLL_INTERVAL}s", flush=True)
        while True:
            try:
                days = fetch_all_days()
                write_events(days)
                total = sum(len(v) for v in days.values())
                print(f"Fetched {total} events across {len(days)} days", flush=True)
            except Exception as e:
                print(f"Fetch error: {e}", flush=True)
            time.sleep(POLL_INTERVAL)

    elif cmd == "once":
        days = fetch_all_days()
        write_events(days)
        total = sum(len(v) for v in days.values())
        print(f"Fetched {total} events across {len(days)} days", flush=True)

    else:
        # Positional day offsets → print JSON to stdout
        offsets = [int(x) for x in args]
        clients = connect_sources()
        if len(offsets) == 1:
            print(json.dumps(fetch_day(clients, offsets[0])))
        else:
            result = {}
            for off in offsets:
                result[str(off)] = fetch_day(clients, off)
            print(json.dumps(result))


if __name__ == "__main__":
    main()
