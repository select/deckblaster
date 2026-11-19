package calfetch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	prefetchStart = -3
	prefetchEnd   = 3
	pollInterval  = 300 * time.Second
)

var windowsTZ = map[string]string{
	"W. Europe Standard Time":      "Europe/Berlin",
	"Central Europe Standard Time": "Europe/Budapest",
	"Romance Standard Time":        "Europe/Paris",
	"GMT Standard Time":            "Europe/London",
	"Eastern Standard Time":        "America/New_York",
	"Central Standard Time":        "America/Chicago",
	"Mountain Standard Time":       "America/Denver",
	"Pacific Standard Time":        "America/Los_Angeles",
	"UTC":                          "UTC",
}

var (
	locCacheMu sync.Mutex
	locCache   = map[string]*time.Location{}
)

func loadLocationCached(name string) (*time.Location, error) {
	locCacheMu.Lock()
	loc, ok := locCache[name]
	locCacheMu.Unlock()
	if ok {
		return loc, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locCacheMu.Lock()
	locCache[name] = loc
	locCacheMu.Unlock()
	return loc, nil
}

// Event is the JSON schema consumed by the calendar widgets.
type Event struct {
	Dt       string `json:"dt"`
	EndDt    string `json:"end_dt"`
	Summary  string `json:"summary"`
	Location string `json:"location"`
}

type eventsPayload struct {
	Days      map[string][]Event `json:"days"`
	FetchedAt int64              `json:"fetchedAt"`
}

func cacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "share", "deckblaster")
}

func eventsFilePath() string {
	return filepath.Join(cacheDir(), "calendar-day-events.json")
}

func icalTimeToUTC(year, month, day, hour, min, sec int, isUTC bool, tzid string) time.Time {
	if tzid != "" {
		iana := tzid
		if mapped, ok := windowsTZ[tzid]; ok {
			iana = mapped
		}
		if loc, err := loadLocationCached(iana); err == nil {
			return time.Date(year, time.Month(month), day, hour, min, sec, 0, loc).UTC()
		}
	}
	if isUTC {
		return time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC)
	}
	return time.Date(year, time.Month(month), day, hour, min, sec, 0, time.Local).UTC()
}

func formatISO8601UTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05+00:00")
}

func dedupAndSortEvents(events []Event) []Event {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Dt < events[j].Dt
	})
	type dedupKey struct {
		dt, endDt, summary string
	}
	seen := make(map[dedupKey]struct{}, len(events))
	unique := make([]Event, 0, len(events))
	for _, e := range events {
		k := dedupKey{dt: e.Dt, endDt: e.EndDt, summary: e.Summary}
		if _, exists := seen[k]; !exists {
			seen[k] = struct{}{}
			unique = append(unique, e)
		}
	}
	return unique
}

func fetchDay(clients *edsClients, dayOffset int, now time.Time) []Event {
	localNow := now.In(time.Local)
	target := localNow.AddDate(0, 0, dayOffset)
	dayStart := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, localNow.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)

	dayStartUTC := dayStart.UTC()
	dayEndUTC := dayEnd.UTC()

	startISO := dayStartUTC.Format("20060102T150405Z")
	endISO := dayEndUTC.Format("20060102T150405Z")
	query := fmt.Sprintf(`(occur-in-time-range? (make-time "%s") (make-time "%s"))`, startISO, endISO)

	raws := clients.queryRange(query, dayStartUTC.Unix(), dayEndUTC.Unix())
	events := make([]Event, 0, len(raws))
	for _, r := range raws {
		dtStart := icalTimeToUTC(r.sy, r.sm, r.sd, r.sh, r.smin, r.ss, r.sisUTC, r.startTZ)
		dtEnd := icalTimeToUTC(r.ey, r.em, r.ed, r.eh, r.emin, r.es, r.eisUTC, r.endTZ)
		events = append(events, Event{
			Dt:       formatISO8601UTC(dtStart),
			EndDt:    formatISO8601UTC(dtEnd),
			Summary:  r.summary,
			Location: r.location,
		})
	}
	return dedupAndSortEvents(events)
}

func fetchAllDays() (map[string][]Event, error) {
	clients, err := connectEDSSources()
	if err != nil {
		return nil, err
	}
	defer clients.close()

	now := time.Now()
	result := make(map[string][]Event, prefetchEnd-prefetchStart+1)
	for offset := prefetchStart; offset <= prefetchEnd; offset++ {
		result[strconv.Itoa(offset)] = fetchDay(clients, offset, now)
	}
	return result, nil
}

func writeEventsToPath(path string, days map[string][]Event, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := eventsPayload{
		Days:      days,
		FetchedAt: now.UnixMilli(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeEvents(days map[string][]Event) error {
	return writeEventsToPath(eventsFilePath(), days, time.Now())
}

// RunCalFetch is the calendar event fetching CLI entrypoint.
func RunCalFetch(args []string) int {
	cmd := "once"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "poll":
		fmt.Printf("Calendar fetcher started, polling every %ds\n", int(pollInterval.Seconds()))
		for {
			days, err := fetchAllDays()
			if err != nil {
				fmt.Printf("Fetch error: %v\n", err)
			} else if err := writeEvents(days); err != nil {
				fmt.Printf("Fetch error: %v\n", err)
			} else {
				total := 0
				for _, v := range days {
					total += len(v)
				}
				fmt.Printf("Fetched %d events across %d days\n", total, len(days))
			}
			time.Sleep(pollInterval)
		}

	case "once":
		days, err := fetchAllDays()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fetch error: %v\n", err)
			return 1
		}
		if err := writeEvents(days); err != nil {
			fmt.Fprintf(os.Stderr, "Write error: %v\n", err)
			return 1
		}
		total := 0
		for _, v := range days {
			total += len(v)
		}
		fmt.Printf("Fetched %d events across %d days\n", total, len(days))
		return 0

	default:
		offsets := make([]int, 0, len(args))
		for _, a := range args {
			off, err := strconv.Atoi(a)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid day offset %q: %v\n", a, err)
				return 1
			}
			offsets = append(offsets, off)
		}
		clients, err := connectEDSSources()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Connect error: %v\n", err)
			return 1
		}
		defer clients.close()

		now := time.Now()
		if len(offsets) == 1 {
			b, err := json.Marshal(fetchDay(clients, offsets[0], now))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Marshal error: %v\n", err)
				return 1
			}
			fmt.Println(string(b))
		} else {
			res := make(map[string][]Event, len(offsets))
			for _, off := range offsets {
				res[strconv.Itoa(off)] = fetchDay(clients, off, now)
			}
			b, err := json.Marshal(res)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Marshal error: %v\n", err)
				return 1
			}
			fmt.Println(string(b))
		}
		return 0
	}
}
