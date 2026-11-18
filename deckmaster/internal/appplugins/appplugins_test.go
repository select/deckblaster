package appplugins

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func assertValid72PNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("expected PNG at %s: %v", path, err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("invalid PNG at %s: %v", path, err)
	}
	if cfg.Width != 72 || cfg.Height != 72 {
		t.Fatalf("expected 72x72 PNG at %s, got %dx%d", path, cfg.Width, cfg.Height)
	}
}

type mockDeckServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	pushed map[int][]string
}

func newMockDeckServer() *mockDeckServer {
	m := &mockDeckServer{pushed: map[int][]string{}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/key/") {
			keyStr := strings.TrimPrefix(r.URL.Path, "/key/")
			k, err := strconv.Atoi(keyStr)
			if err == nil {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				icon, _ := body["icon"].(string)
				m.mu.Lock()
				m.pushed[k] = append(m.pushed[k], icon)
				m.mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	return m
}

func (m *mockDeckServer) close() {
	m.srv.Close()
}

func TestCalcOperationsAndRendering(t *testing.T) {
	srv := newMockDeckServer()
	defer srv.close()
	origAPI := apiBase
	apiBase = srv.srv.URL
	defer func() { apiBase = origAPI }()

	tmp := t.TempDir()
	origState := calcStateFile
	origTmp := calcTmpDir
	calcStateFile = filepath.Join(tmp, "calc.json")
	calcTmpDir = filepath.Join(tmp, "calc-imgs")
	defer func() {
		calcStateFile = origState
		calcTmpDir = origTmp
	}()

	// Chained: 3 * 4 + 2 = 14
	if code := RunCalc([]string{"init"}); code != 0 {
		t.Fatalf("init failed: %d", code)
	}
	RunCalc([]string{"digit", "3"})
	RunCalc([]string{"op", "*"})
	RunCalc([]string{"digit", "4"})
	RunCalc([]string{"op", "+"})
	s := loadCalcState()
	if s.Display != "12" {
		t.Fatalf("expected intermediate chained display '12', got %q", s.Display)
	}
	RunCalc([]string{"digit", "2"})
	RunCalc([]string{"equals"})
	s = loadCalcState()
	if s.Display != "14" || s.Error {
		t.Fatalf("expected '14', got %q (error=%v)", s.Display, s.Error)
	}
	assertValid72PNG(t, filepath.Join(calcTmpDir, "display.png"))

	// 12-digit max input
	RunCalc([]string{"clear"})
	for i := 0; i < 15; i++ {
		RunCalc([]string{"digit", "9"})
	}
	s = loadCalcState()
	if len(s.Display) != 12 {
		t.Fatalf("expected max 12 digits, got %d (%q)", len(s.Display), s.Display)
	}

	// Division by zero -> ERR
	RunCalc([]string{"clear"})
	RunCalc([]string{"digit", "8"})
	RunCalc([]string{"op", "/"})
	RunCalc([]string{"digit", "0"})
	RunCalc([]string{"equals"})
	s = loadCalcState()
	if s.Display != "ERR" || !s.Error {
		t.Fatalf("expected ERR state, got %q (error=%v)", s.Display, s.Error)
	}

	// Only key 0 should ever be pushed
	for k := range srv.pushed {
		if k != 0 {
			t.Fatalf("unexpected push to key %d", k)
		}
	}
	if len(srv.pushed[0]) == 0 {
		t.Fatalf("expected pushes to key 0")
	}
}

func TestHighlightRenderingAndState(t *testing.T) {
	srv := newMockDeckServer()
	defer srv.close()
	origAPI := apiBase
	apiBase = srv.srv.URL
	defer func() { apiBase = origAPI }()

	tmp := t.TempDir()
	origState := highlightStateFile
	origPID := highlightPIDFile
	origTmp := highlightTmpDir
	origWal := highlightWalFile
	highlightStateFile = filepath.Join(tmp, "highlight.json")
	highlightPIDFile = filepath.Join(tmp, "highlight.pid")
	highlightTmpDir = filepath.Join(tmp, "highlight-imgs")
	highlightWalFile = filepath.Join(tmp, "wal.json")
	defer func() {
		highlightStateFile = origState
		highlightPIDFile = origPID
		highlightTmpDir = origTmp
		highlightWalFile = origWal
	}()

	walJSON := `{"colors":{"color1":"#112233","color2":"#223344","color3":"#334455","color4":"#445566","color5":"#556677","color6":"#667788","color7":"#778899"}}`
	if err := os.WriteFile(highlightWalFile, []byte(walJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	colors := loadWalColors()
	if colors[0].Hex != "#112233" || len(colors) != 7 {
		t.Fatalf("unexpected wal colors: %+v", colors)
	}

	t.Setenv("XDG_SESSION_TYPE", "x11")

	if code := RunHighlight([]string{"init"}); code != 0 {
		t.Fatalf("highlight init failed: %d", code)
	}
	expectedBtns := []string{
		"status.png",
		"btn-size-up.png",
		"btn-size-down.png",
		"btn-color-next.png",
		"btn-color-prev.png",
		"btn-alpha-up.png",
		"btn-alpha-down.png",
		"btn-mode.png",
	}
	for _, b := range expectedBtns {
		assertValid72PNG(t, filepath.Join(highlightTmpDir, b))
	}

	RunHighlight([]string{"size-up"})
	RunHighlight([]string{"color-next"})
	RunHighlight([]string{"opacity-up"})
	RunHighlight([]string{"mode"})
	s := loadHighlightState()
	if s.RadiusIdx != 3 || s.ColorIdx != 1 || s.OpacityIdx != 4 || s.Mode != "click-only" {
		t.Fatalf("unexpected highlight state: %+v", s)
	}

	if code := RunHighlight([]string{"btn-icon", "alpha-down"}); code != 0 {
		t.Fatalf("btn-icon failed: %d", code)
	}
	assertValid72PNG(t, filepath.Join(highlightTmpDir, "btn-alpha-down.png"))
}

func TestSlotsPayoutsBetIconAndSafety(t *testing.T) {
	srv := newMockDeckServer()
	defer srv.close()
	origAPI := apiBase
	apiBase = srv.srv.URL
	defer func() { apiBase = origAPI }()

	tmp := t.TempDir()
	origState := slotsStateFile
	origTmp := slotsTmpDir
	origSleep := slotsSleep
	slotsStateFile = filepath.Join(tmp, "slots.json")
	slotsTmpDir = filepath.Join(tmp, "slots-imgs")
	slotsSleep = func(time.Duration) {}
	defer func() {
		slotsStateFile = origState
		slotsTmpDir = origTmp
		slotsSleep = origSleep
	}()

	// Payout check: 3 sevens on top row + 2 cherries on middle row
	grid := [][]string{
		{"seven", "seven", "seven"},
		{"cherry", "cherry", "lemon"},
		{"bell", "star", "bar"},
	}
	// Note: diagonal (0,0)-(1,1)-(2,2) has seven, cherry, bar (no match)
	// diagonal (2,0)-(1,1)-(0,2) has bell, cherry, seven (no match)
	// top: 3x seven = 77 * 5 = 385
	// mid: 2x cherry = max(1, 5/3) * 5 = 1 * 5 = 5
	// total = 390
	win, winners := checkSlotsWins(grid, 5)
	if win != 390 {
		t.Fatalf("expected win 390, got %d", win)
	}
	if len(winners) != 5 {
		t.Fatalf("expected 5 winning cells, got %d", len(winners))
	}

	if code := RunSlots([]string{"init"}); code != 0 {
		t.Fatalf("slots init failed: %d", code)
	}
	assertValid72PNG(t, filepath.Join(slotsTmpDir, "bet-display.png"))
	assertValid72PNG(t, filepath.Join(slotsTmpDir, "credits.png"))

	RunSlots([]string{"bet-up"})
	if s := loadSlotsState(); s.Bet != 10 {
		t.Fatalf("expected bet 10 after bet-up, got %d", s.Bet)
	}
	RunSlots([]string{"bet-down"})
	if s := loadSlotsState(); s.Bet != 5 {
		t.Fatalf("expected bet 5 after bet-down, got %d", s.Bet)
	}
	if code := RunSlots([]string{"bet-icon"}); code != 0 {
		t.Fatalf("bet-icon failed: %d", code)
	}
	assertValid72PNG(t, filepath.Join(slotsTmpDir, "bet-display.png"))

	// Spin must NEVER push to action keys (4, 5, 9, 10)
	if code := RunSlots([]string{"spin"}); code != 0 {
		t.Fatalf("slots spin failed: %d", code)
	}
	for _, forbidden := range []int{4, 5, 9, 10} {
		if len(srv.pushed[forbidden]) > 0 {
			t.Fatalf("forbidden push to action key %d: %v", forbidden, srv.pushed[forbidden])
		}
	}
}

func TestPolymarketFetchRenderAndCategories(t *testing.T) {
	gammaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/markets"):
			_, _ = w.Write([]byte(`[
				{"question":"Will Bitcoin reach 150k by December 2026?","outcomePrices":"[\"0.42\",\"0.58\"]","volume":"2500000"},
				{"question":"Will AI pass the Turing test in 2026?","outcomePrices":"[\"0.65\",\"0.35\"]","volume":"120000"},
				{"question":"Will Fusion power plant open?","outcomePrices":"[\"0.15\",\"0.85\"]","volume":"900"},
				{"question":"Extreme settled market","outcomePrices":"[\"0.99\",\"0.01\"]","volume":"500000"}
			]`))
		case strings.HasPrefix(r.URL.Path, "/events"):
			if strings.Contains(r.URL.RawQuery, "offset=0") && !strings.Contains(r.URL.RawQuery, "tag_id=") {
				_, _ = w.Write([]byte(`[
					{"volume24hr":50000,"tags":[{"label":"Crypto","id":"101"},{"label":"NBA","id":"999"}]},
					{"volume24hr":30000,"tags":[{"label":"Crypto","id":"101"},{"label":"Tech","id":202}]},
					{"volume24hr":10000,"tags":[{"label":"Tech","id":202}]}
				]`))
			} else if strings.Contains(r.URL.RawQuery, "tag_id=") {
				_, _ = w.Write([]byte(`[
					{"markets":[{"question":"Crypto market question one?","outcomePrices":"[\"0.55\",\"0.45\"]","volume":"80000"}]}
				]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer gammaSrv.Close()

	tmp := t.TempDir()
	origGamma := gammaAPIBase
	origData := polymarketDataDir
	origCache := polymarketCacheBase
	origTmp := polymarketTmpDir
	gammaAPIBase = gammaSrv.URL
	polymarketDataDir = filepath.Join(tmp, "data")
	polymarketCacheBase = filepath.Join(tmp, "cache")
	polymarketTmpDir = filepath.Join(tmp, "imgs")
	defer func() {
		gammaAPIBase = origGamma
		polymarketDataDir = origData
		polymarketCacheBase = origCache
		polymarketTmpDir = origTmp
	}()

	if code := RunPolymarket([]string{"init"}); code != 0 {
		t.Fatalf("polymarket init failed: %d", code)
	}
	assertValid72PNG(t, filepath.Join(polymarketTmpDir, "info.png"))
	for i := 0; i < 3; i++ {
		assertValid72PNG(t, filepath.Join(polymarketTmpDir, "yes-"+strconv.Itoa(i)+".png"))
		assertValid72PNG(t, filepath.Join(polymarketTmpDir, "no-"+strconv.Itoa(i)+".png"))
		assertValid72PNG(t, filepath.Join(polymarketTmpDir, "text-"+strconv.Itoa(i)+".png"))
	}

	if code := RunPolymarket([]string{"more", "0"}); code != 0 {
		t.Fatalf("more failed: %d", code)
	}
	if code := RunPolymarket([]string{"render-text", "0"}); code != 0 {
		t.Fatalf("render-text failed: %d", code)
	}

	if code := RunPolymarket([]string{"cat-init"}); code != 0 {
		t.Fatalf("cat-init failed: %d", code)
	}
	assertValid72PNG(t, filepath.Join(polymarketTmpDir, "cat-page.png"))
	for i := 0; i < 9; i++ {
		assertValid72PNG(t, filepath.Join(polymarketTmpDir, "cat-"+strconv.Itoa(i)+".png"))
	}

	// Verify sports tag "NBA" was filtered out, while "Popular", "Crypto", "Tech" remain
	catsData, err := os.ReadFile(polymarketCatsFile())
	if err != nil {
		t.Fatal(err)
	}
	var cats []categoryItem
	if err := json.Unmarshal(catsData, &cats); err != nil {
		t.Fatal(err)
	}
	if len(cats) != 3 || cats[0].Name != "Popular" {
		t.Fatalf("expected 3 categories starting with Popular, got %+v", cats)
	}

	// Select Crypto (slot 1)
	if code := RunPolymarket([]string{"select-cat", "1"}); code != 0 {
		t.Fatalf("select-cat failed: %d", code)
	}
	s := loadPolymarketState()
	if s.CatName != "Crypto" || s.CatVisits["Crypto"] != 1 {
		t.Fatalf("unexpected state after select-cat: %+v", s)
	}

	// Reset cat page
	s.CatPage = 2
	savePolymarketState(s)
	if code := RunPolymarket([]string{"reset-cat-page"}); code != 0 {
		t.Fatalf("reset-cat-page failed: %d", code)
	}
	if s2 := loadPolymarketState(); s2.CatPage != 0 {
		t.Fatalf("expected cat_page 0 after reset-cat-page, got %d", s2.CatPage)
	}
}
