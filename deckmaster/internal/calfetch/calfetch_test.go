package calfetch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestICalTimeToUTC(t *testing.T) {
	// Windows timezone mapping (Europe/Berlin is UTC+2 in summer)
	got := icalTimeToUTC(2026, 7, 15, 14, 30, 0, false, "W. Europe Standard Time")
	want := time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Windows TZ: got %v, want %v", got, want)
	}

	// IANA timezone
	got = icalTimeToUTC(2026, 1, 15, 10, 0, 0, false, "America/New_York")
	want = time.Date(2026, 1, 15, 15, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("IANA TZ: got %v, want %v", got, want)
	}

	// Explicit UTC flag
	got = icalTimeToUTC(2026, 4, 19, 8, 15, 0, true, "")
	want = time.Date(2026, 4, 19, 8, 15, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("isUTC: got %v, want %v", got, want)
	}

	// Unknown TZ falls back to local/UTC without panicking
	got = icalTimeToUTC(2026, 4, 19, 8, 15, 0, true, "Unknown/Zone")
	if !got.Equal(want) {
		t.Fatalf("unknown TZ with isUTC: got %v, want %v", got, want)
	}
}

func TestDedupAndSortEvents(t *testing.T) {
	in := []Event{
		{Dt: "2026-04-19T12:00:00+00:00", EndDt: "2026-04-19T13:00:00+00:00", Summary: "Lunch", Location: ""},
		{Dt: "2026-04-19T09:00:00+00:00", EndDt: "2026-04-19T09:30:00+00:00", Summary: "Standup", Location: "Zoom"},
		{Dt: "2026-04-19T09:00:00+00:00", EndDt: "2026-04-19T09:30:00+00:00", Summary: "Standup", Location: "Duplicate"},
	}
	got := dedupAndSortEvents(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Summary != "Standup" || got[1].Summary != "Lunch" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestWriteEventsToPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "calendar-day-events.json")
	days := map[string][]Event{
		"0": {},
		"1": {{Dt: "2026-04-20T10:00:00+00:00", EndDt: "2026-04-20T11:00:00+00:00", Summary: "Sync", Location: ""}},
	}
	now := time.UnixMilli(1700000000123)
	if err := writeEventsToPath(path, days, now); err != nil {
		t.Fatalf("writeEventsToPath failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var parsed eventsPayload
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed.FetchedAt != 1700000000123 {
		t.Fatalf("fetchedAt mismatch: %d", parsed.FetchedAt)
	}
	if len(parsed.Days["0"]) != 0 || parsed.Days["0"] == nil {
		t.Fatalf("expected empty non-nil slice for day 0, got %#v", parsed.Days["0"])
	}
	if len(parsed.Days["1"]) != 1 || parsed.Days["1"][0].Summary != "Sync" {
		t.Fatalf("unexpected day 1: %+v", parsed.Days["1"])
	}
}

func TestRunCalFetchLiveParity(t *testing.T) {
	clients, err := connectEDSSources()
	if err != nil {
		t.Skipf("EDS unavailable in test env: %v", err)
	}
	defer clients.close()
	events := fetchDay(clients, 0, time.Now())
	if events == nil {
		t.Fatal("expected non-nil slice")
	}
	if parityPath := os.Getenv("CALFETCH_PARITY_JSON"); parityPath != "" {
		raw, err := os.ReadFile(parityPath)
		if err != nil {
			t.Fatalf("read parity file: %v", err)
		}
		var pyEvents []Event
		if err := json.Unmarshal(raw, &pyEvents); err != nil {
			t.Fatalf("unmarshal parity file: %v", err)
		}
		goJSON, _ := json.Marshal(events)
		pyJSON, _ := json.Marshal(pyEvents)
		if string(goJSON) != string(pyJSON) {
			t.Fatalf("parity mismatch:\nGo: %s\nPy: %s", goJSON, pyJSON)
		}
		t.Logf("100%% parity verified (%d events)", len(events))
	}
}
