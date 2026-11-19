package appplugins

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	gammaAPIBase        = "https://gamma-api.polymarket.com"
	polymarketDataDir   = expandHome("~/.local/share/streamdeck-polymarket")
	polymarketCacheBase = "/tmp/streamdeck-polymarket-cache"
	polymarketTmpDir    = "/tmp/streamdeck-polymarket"
	polymarketHTTP      = &http.Client{Timeout: 15 * time.Second}
)

const (
	betsPerPage    = 3
	totalFetch     = 60
	cacheTTL       = 300.0
	catsCacheTTL   = 600.0
	catsPerPageSel = 9
	textGridCols   = 9
	textGridRows   = 4
	textScrollStep = 1
	textCharW      = 7
	textCharH      = 13
)

var sportsTags = map[string]bool{
	"sports": true, "soccer": true, "nba": true, "nfl": true, "mlb": true, "nhl": true,
	"tennis": true, "golf": true, "mma": true, "ufc": true, "boxing": true,
	"epl": true, "la liga": true, "serie a": true, "bundesliga": true, "ligue 1": true,
	"champions league": true, "f1": true, "college football playoffs": true, "cricket": true,
	"major league cricket": true, "hockey": true, "europa league": true,
	"investec champions cup": true, "ecf mvp": true, "nl central": true,
	"ohtani": true, "qb": true, "green bay packers": true, "caitlin clark": true,
	"florida panthers": true, "san jose sharks": true, "mavericks": true, "bat": true,
	"egypt premier league": true, "nba finals": true, "basketball": true, "nba champion": true,
	"mvp": true, "stanley cup": true, "ucl": true, "fifa world cup": true,
	"2026 fifa world cup": true,
}

var skipTags = map[string]bool{
	"earn 4%": true, "hide from new": true, "pre-market": true, "token launch": true,
	"governor midterms": true, "senate midterms": true, "2025 predictions": true,
	"product marekt fit": true, "virgins": true, "macro election 2": true,
}

type polymarketState struct {
	Page      int            `json:"page"`
	Total     int            `json:"total"`
	Frames    []int          `json:"frames"`
	CatIdx    int            `json:"cat_idx"`
	CatPage   int            `json:"cat_page"`
	CatName   string         `json:"cat_name,omitempty"`
	CatQuery  *string        `json:"cat_query,omitempty"`
	CatTagID  *string        `json:"cat_tag_id,omitempty"`
	CatVisits map[string]int `json:"cat_visits,omitempty"`
	Loading   bool           `json:"loading,omitempty"`
}

type marketItem struct {
	Question string  `json:"question"`
	Yes      float64 `json:"yes"`
	No       float64 `json:"no"`
	Volume   float64 `json:"volume"`
}

type marketCacheFile struct {
	TS      float64      `json:"ts"`
	Markets []marketItem `json:"markets"`
}

type categoryItem struct {
	Name   string  `json:"name"`
	TagID  *string `json:"tag_id"`
	Query  *string `json:"query"`
	Count  int     `json:"count,omitempty"`
	Visits int     `json:"visits"`
}

func polymarketStateFile() string {
	return filepath.Join(polymarketDataDir, "state.json")
}

func polymarketCatsFile() string {
	return filepath.Join(polymarketDataDir, "categories.json")
}

func defaultPolymarketState() polymarketState {
	return polymarketState{
		Page:   0,
		Total:  0,
		Frames: []int{0, 0, 0},
		CatIdx: 0,
	}
}

func loadPolymarketState() polymarketState {
	data, err := os.ReadFile(polymarketStateFile())
	if err != nil {
		return defaultPolymarketState()
	}
	var s polymarketState
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultPolymarketState()
	}
	if len(s.Frames) < 3 {
		s.Frames = []int{0, 0, 0}
	}
	return s
}

func savePolymarketState(s polymarketState) {
	if err := writeJSONAtomic(polymarketStateFile(), s); err != nil {
		fmt.Fprintf(os.Stderr, "polymarket save state error: %v\n", err)
	}
}

func anyToFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err == nil {
			return f
		}
	case json.Number:
		f, err := x.Float64()
		if err == nil {
			return f
		}
	}
	return 0
}

func anyToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	case json.Number:
		return x.String()
	}
	return ""
}

func marketCachePath(query, tagID *string) string {
	key := "popular"
	if tagID != nil && *tagID != "" {
		key = *tagID
	} else if query != nil && *query != "" {
		key = *query
	}
	key = strings.ReplaceAll(key, " ", "_")
	return fmt.Sprintf("%s-%s.json", polymarketCacheBase, key)
}

func parseRawMarket(raw map[string]any) (marketItem, bool) {
	opRaw, ok := raw["outcomePrices"]
	if !ok || opRaw == nil {
		return marketItem{}, false
	}
	var prices []any
	switch v := opRaw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &prices); err != nil {
			return marketItem{}, false
		}
	case []any:
		prices = v
	default:
		return marketItem{}, false
	}
	if len(prices) == 0 {
		return marketItem{}, false
	}
	yesPrice := anyToFloat(prices[0])
	if yesPrice <= 0.02 || yesPrice >= 0.98 {
		return marketItem{}, false
	}
	q, _ := raw["question"].(string)
	if q == "" {
		q = "?"
	}
	vol := anyToFloat(raw["volume"])
	return marketItem{
		Question: q,
		Yes:      yesPrice,
		No:       1.0 - yesPrice,
		Volume:   vol,
	}, true
}

func fetchJSON(reqURL string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "StreamDeck/1.0")
	resp, err := polymarketHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func fetchMarkets(force bool, query, tagID *string) []marketItem {
	cPath := marketCachePath(query, tagID)
	now := float64(time.Now().UnixNano()) / 1e9

	if !force {
		if data, err := os.ReadFile(cPath); err == nil {
			var cache marketCacheFile
			if err := json.Unmarshal(data, &cache); err == nil {
				if now-cache.TS < cacheTTL {
					return cache.Markets
				}
			}
		}
	}

	var rawMarkets []map[string]any
	var fetchErr error

	if tagID != nil && *tagID != "" {
		u := fmt.Sprintf("%s/events?active=true&closed=false&tag_id=%s&limit=%d&order=volume24hr&ascending=false",
			gammaAPIBase, url.QueryEscape(*tagID), totalFetch)
		var events []struct {
			Markets []map[string]any `json:"markets"`
		}
		if fetchErr = fetchJSON(u, &events); fetchErr == nil {
			for _, ev := range events {
				rawMarkets = append(rawMarkets, ev.Markets...)
			}
		}
	} else if query != nil && *query != "" {
		u := fmt.Sprintf("%s/public-search?q=%s&limit=%d",
			gammaAPIBase, url.QueryEscape(*query), totalFetch)
		var resp struct {
			Events []struct {
				Markets []map[string]any `json:"markets"`
			} `json:"events"`
		}
		if fetchErr = fetchJSON(u, &resp); fetchErr == nil {
			for _, ev := range resp.Events {
				rawMarkets = append(rawMarkets, ev.Markets...)
			}
		}
	} else {
		u := fmt.Sprintf("%s/markets?active=true&closed=false&limit=%d&order=volume24hr&ascending=false",
			gammaAPIBase, totalFetch)
		fetchErr = fetchJSON(u, &rawMarkets)
	}

	if fetchErr != nil {
		fmt.Fprintf(os.Stderr, "Fetch error: %v\n", fetchErr)
		if data, err := os.ReadFile(cPath); err == nil {
			var cache marketCacheFile
			if err := json.Unmarshal(data, &cache); err == nil {
				return cache.Markets
			}
		}
		return nil
	}

	markets := make([]marketItem, 0, len(rawMarkets))
	for _, rm := range rawMarkets {
		if m, ok := parseRawMarket(rm); ok {
			markets = append(markets, m)
		}
	}
	sort.Slice(markets, func(i, j int) bool {
		return markets[i].Volume > markets[j].Volume
	})

	_ = writeJSONAtomic(cPath, marketCacheFile{TS: now, Markets: markets})
	return markets
}

func formatVolume(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("$%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("$%.0fK", v/1_000)
	}
	return fmt.Sprintf("$%.0f", v)
}

func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	var lines []string
	current := ""
	for _, w := range words {
		if current == "" {
			current = w
		} else if len(current)+1+len(w) <= width {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func renderPolymarketYes(m marketItem, idx int) string {
	img := newRGBA(color.RGBA{R: 8, G: 18, B: 8, A: 255})
	pct := int(m.Yes * 100)
	barH := int(60 * m.Yes)
	if barH < 1 {
		barH = 1
	}
	yStart := keySize - 4 - barH
	for y := yStart; y < keySize-4; y++ {
		t := float64(y-yStart) / float64(barH)
		c := color.RGBA{
			R: uint8(15 + 15*t),
			G: uint8(80 + 80*t),
			B: uint8(25 + 25*t),
			A: 255,
		}
		for x := 4; x < keySize-4; x++ {
			blendPixel(img, x, y, c)
		}
	}

	drawTextTopLeft(img, 6, 4, "YES", color.RGBA{R: 100, G: 220, B: 120, A: 255}, getFontFace(fontDejaVuSansBold, 10))
	pctText := fmt.Sprintf("%d%%", pct)
	faceLG := getFontFace(fontDejaVuSansBold, 26)
	tw, th := measureText(faceLG, pctText)
	drawTextTopLeft(img, (keySize-tw)/2, (keySize-th)/2+2, pctText, color.RGBA{R: 150, G: 255, B: 160, A: 255}, faceLG)

	out := color.RGBA{R: 40, G: 100, B: 50, A: 255}
	drawRoundedRect(img, 1, 1, 71, 71, 6, nil, &out, 1)

	path := filepath.Join(polymarketTmpDir, fmt.Sprintf("yes-%d.png", idx))
	_ = savePNGAtomic(path, img)
	return path
}

func renderPolymarketNo(m marketItem, idx int) string {
	img := newRGBA(color.RGBA{R: 18, G: 8, B: 8, A: 255})
	pct := int(m.No * 100)
	barH := int(60 * m.No)
	if barH < 1 {
		barH = 1
	}
	for y := 4; y < 4+barH; y++ {
		t := float64(y-4) / float64(barH)
		c := color.RGBA{
			R: uint8(160 - 80*t),
			G: uint8(30 - 15*t),
			B: uint8(30 - 15*t),
			A: 255,
		}
		for x := 4; x < keySize-4; x++ {
			blendPixel(img, x, y, c)
		}
	}

	pctText := fmt.Sprintf("%d%%", pct)
	faceLG := getFontFace(fontDejaVuSansBold, 26)
	tw, th := measureText(faceLG, pctText)
	drawTextTopLeft(img, (keySize-tw)/2, (keySize-th)/2-2, pctText, color.RGBA{R: 255, G: 140, B: 140, A: 255}, faceLG)
	drawTextTopLeft(img, 48, 56, "NO", color.RGBA{R: 220, G: 100, B: 100, A: 255}, getFontFace(fontDejaVuSansBold, 10))

	out := color.RGBA{R: 100, G: 40, B: 40, A: 255}
	drawRoundedRect(img, 1, 1, 71, 71, 6, nil, &out, 1)

	path := filepath.Join(polymarketTmpDir, fmt.Sprintf("no-%d.png", idx))
	_ = savePNGAtomic(path, img)
	return path
}

func renderPolymarketEmptyAt(path string) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	_ = savePNGAtomic(path, img)
	return path
}

func renderPolymarketInfo(page, totalPages, totalMarkets int, catName string) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	fill := color.RGBA{R: 15, G: 15, B: 30, A: 255}
	out := color.RGBA{R: 60, G: 80, B: 140, A: 255}
	drawRoundedRect(img, 2, 2, 70, 70, 8, &fill, &out, 2)

	pgText := fmt.Sprintf("%d/%d", page+1, totalPages)
	faceMD := getFontFace(fontDejaVuSansBold, 14)
	tw, _ := measureText(faceMD, pgText)
	drawTextTopLeft(img, (keySize-tw)/2, 4, pgText, color.RGBA{R: 100, G: 160, B: 255, A: 255}, faceMD)

	faceSM := getFontFace(fontDejaVuSansBold, 10)
	tw2, _ := measureText(faceSM, catName)
	drawTextTopLeft(img, (keySize-tw2)/2, 24, catName, color.RGBA{R: 200, G: 160, B: 255, A: 255}, faceSM)

	faceXS := getFontFace(fontDejaVuSans, 9)
	drawTextTopLeft(img, 6, 40, "Polymarket", color.RGBA{R: 80, G: 80, B: 120, A: 255}, faceXS)
	drawTextTopLeft(img, 6, 56, fmt.Sprintf("%d bets", totalMarkets), color.RGBA{R: 100, G: 100, B: 140, A: 255}, faceXS)

	path := filepath.Join(polymarketTmpDir, "info.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderPolymarketTextSlot(m *marketItem, slot, frame int) string {
	path := filepath.Join(polymarketTmpDir, fmt.Sprintf("text-%d.png", slot))
	if m == nil {
		return renderPolymarketEmptyAt(path)
	}

	allLines := wrapWords(m.Question, textGridCols)
	maxScroll := len(allLines) - textGridRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	totalSteps := 1
	if maxScroll > 0 {
		totalSteps = int(math.Ceil(float64(maxScroll)/float64(textScrollStep))) + 1
		if totalSteps < 1 {
			totalSteps = 1
		}
	}

	currentStep := frame
	if currentStep >= totalSteps {
		currentStep = totalSteps - 1
	}
	if currentStep < 0 {
		currentStep = 0
	}
	scrollPos := currentStep * textScrollStep
	if scrollPos > maxScroll {
		scrollPos = maxScroll
	}
	end := scrollPos + textGridRows
	if end > len(allLines) {
		end = len(allLines)
	}
	visible := allLines[scrollPos:end]

	img := newRGBA(color.RGBA{R: 12, G: 12, B: 20, A: 255})
	out := color.RGBA{R: 50, G: 50, B: 70, A: 255}
	drawRoundedRect(img, 1, 1, 71, 71, 6, nil, &out, 1)

	volText := formatVolume(m.Volume)
	faceXS := getFontFace(fontDejaVuSansMono, 9)
	tw, _ := measureText(faceXS, volText)
	drawTextTopLeft(img, (keySize-tw)/2, 2, volText, color.RGBA{R: 120, G: 120, B: 160, A: 255}, faceXS)

	faceTxt := getFontFace(fontDejaVuSansMonoBold, 12)
	y := 14
	xOffset := (keySize - textGridCols*textCharW) / 2
	for _, line := range visible {
		drawTextTopLeft(img, xOffset, y, line, color.RGBA{R: 220, G: 220, B: 240, A: 255}, faceTxt)
		y += textCharH
	}

	if totalSteps > 1 {
		// Keep page indicators compact and below the text so they never obscure it.
		lineWidth := 5
		lineGap := 2
		for totalSteps*lineWidth+(totalSteps-1)*lineGap > 56 && lineWidth > 1 {
			lineWidth--
		}
		for totalSteps*lineWidth+(totalSteps-1)*lineGap > 56 && lineGap > 1 {
			lineGap--
		}
		indicatorWidth := totalSteps*lineWidth + (totalSteps-1)*lineGap
		startX := (keySize - indicatorWidth) / 2
		indicatorY := keySize - 5
		for i := 0; i < totalSteps; i++ {
			c := color.RGBA{R: 65, G: 72, B: 95, A: 255}
			if i == currentStep {
				c = color.RGBA{R: 100, G: 160, B: 255, A: 255}
			}
			x := startX + i*(lineWidth+lineGap)
			for py := indicatorY; py < indicatorY+2; py++ {
				for px := x; px < x+lineWidth; px++ {
					blendPixel(img, px, py, c)
				}
			}
		}
	}

	_ = savePNGAtomic(path, img)
	return path
}

func clearOldPolymarketImages() {
	patterns := []string{"bet-*.png", "text-*.png", "yes-*.png", "no-*.png", "info*.png"}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(polymarketTmpDir, pat))
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}

func getPolymarketCategory(s polymarketState) (string, *string, *string) {
	name := s.CatName
	if name == "" {
		name = "Popular"
	}
	return name, s.CatQuery, s.CatTagID
}

func displayPolymarketPage(s *polymarketState, markets []marketItem) {
	totalPages := (len(markets) + betsPerPage - 1) / betsPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	page := s.Page
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	s.Page = page
	s.Total = len(markets)
	s.Frames = []int{0, 0, 0}

	catName, _, _ := getPolymarketCategory(*s)
	renderPolymarketInfo(page, totalPages, len(markets), catName)

	start := page * betsPerPage
	for i := 0; i < betsPerPage; i++ {
		idx := start + i
		if idx < len(markets) {
			m := markets[idx]
			renderPolymarketYes(m, i)
			renderPolymarketNo(m, i)
			renderPolymarketTextSlot(&m, i, 0)
		} else {
			renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, fmt.Sprintf("yes-%d.png", i)))
			renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, fmt.Sprintf("no-%d.png", i)))
			renderPolymarketTextSlot(nil, i, 0)
		}
	}
}

func buildCategories(s polymarketState, force bool) []categoryItem {
	cFile := polymarketCatsFile()
	if !force {
		if st, err := os.Stat(cFile); err == nil {
			if time.Since(st.ModTime()).Seconds() < catsCacheTTL {
				if data, err := os.ReadFile(cFile); err == nil {
					var cats []categoryItem
					if err := json.Unmarshal(data, &cats); err == nil {
						return cats
					}
				}
			}
		}
	}

	visits := s.CatVisits
	if visits == nil {
		visits = map[string]int{}
	}

	popular := categoryItem{
		Name:   "Popular",
		TagID:  nil,
		Query:  nil,
		Visits: visits["Popular"],
	}
	seen := map[string]bool{"popular": true}

	type tagStat struct {
		id     string
		count  int
		vol24h float64
	}
	tagData := map[string]*tagStat{}

	for offset := 0; offset < 1000; offset += 200 {
		u := fmt.Sprintf("%s/events?active=true&closed=false&limit=200&offset=%d&order=volume24hr&ascending=false",
			gammaAPIBase, offset)
		var events []struct {
			Volume24hr any `json:"volume24hr"`
			Tags       []struct {
				Label string `json:"label"`
				ID    any    `json:"id"`
			} `json:"tags"`
		}
		if err := fetchJSON(u, &events); err != nil {
			fmt.Fprintf(os.Stderr, "Category discovery error: %v\n", err)
			break
		}
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			v24 := anyToFloat(e.Volume24hr)
			for _, t := range e.Tags {
				if t.Label == "" {
					continue
				}
				low := strings.ToLower(t.Label)
				if sportsTags[low] || skipTags[low] {
					continue
				}
				st, ok := tagData[t.Label]
				if !ok {
					st = &tagStat{id: anyToString(t.ID)}
					tagData[t.Label] = st
				}
				st.count++
				st.vol24h += v24
			}
		}
	}

	type tagEntry struct {
		label string
		stat  *tagStat
	}
	entries := make([]tagEntry, 0, len(tagData))
	for lbl, st := range tagData {
		entries = append(entries, tagEntry{lbl, st})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].stat.vol24h > entries[j].stat.vol24h
	})

	var rest []categoryItem
	for _, e := range entries {
		low := strings.ToLower(e.label)
		if e.stat.count >= 2 && !seen[low] {
			tid := e.stat.id
			q := e.label
			rest = append(rest, categoryItem{
				Name:   e.label,
				TagID:  &tid,
				Query:  &q,
				Count:  e.stat.count,
				Visits: visits[e.label],
			})
			seen[low] = true
		}
	}

	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].Visits != rest[j].Visits {
			return rest[i].Visits > rest[j].Visits
		}
		return rest[i].Count > rest[j].Count
	})

	maxRest := 50*catsPerPageSel - 1
	if len(rest) > maxRest {
		rest = rest[:maxRest]
	}
	cats := append([]categoryItem{popular}, rest...)
	_ = writeJSONAtomic(cFile, cats)
	return cats
}

func updateCatsVisits(s polymarketState) {
	cFile := polymarketCatsFile()
	data, err := os.ReadFile(cFile)
	if err != nil {
		return
	}
	var cats []categoryItem
	if err := json.Unmarshal(data, &cats); err != nil {
		return
	}
	visits := s.CatVisits
	if visits == nil {
		visits = map[string]int{}
	}
	var popular *categoryItem
	var rest []categoryItem
	for i := range cats {
		cats[i].Visits = visits[cats[i].Name]
		if cats[i].Name == "Popular" && popular == nil {
			c := cats[i]
			popular = &c
		} else {
			rest = append(rest, cats[i])
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].Visits != rest[j].Visits {
			return rest[i].Visits > rest[j].Visits
		}
		return rest[i].Count > rest[j].Count
	})
	out := make([]categoryItem, 0, len(cats))
	if popular != nil {
		out = append(out, *popular)
	}
	out = append(out, rest...)
	_ = writeJSONAtomic(cFile, out)
}

func renderAllCatIcons() {
	sData, err1 := os.ReadFile(polymarketStateFile())
	cData, err2 := os.ReadFile(polymarketCatsFile())
	if err1 != nil || err2 != nil {
		for i := 0; i < catsPerPageSel; i++ {
			renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, fmt.Sprintf("cat-%d.png", i)))
		}
		renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, "cat-page.png"))
		return
	}

	var s polymarketState
	var cats []categoryItem
	if json.Unmarshal(sData, &s) != nil || json.Unmarshal(cData, &cats) != nil {
		for i := 0; i < catsPerPageSel; i++ {
			renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, fmt.Sprintf("cat-%d.png", i)))
		}
		renderPolymarketEmptyAt(filepath.Join(polymarketTmpDir, "cat-page.png"))
		return
	}

	selectedName := s.CatName
	if selectedName == "" {
		selectedName = "Popular"
	}

	totalPages := (len(cats) + catsPerPageSel - 1) / catsPerPageSel
	if totalPages < 1 {
		totalPages = 1
	}
	catPage := s.CatPage
	if catPage < 0 {
		catPage = 0
	}
	if catPage >= totalPages {
		catPage = totalPages - 1
	}
	start := catPage * catsPerPageSel
	end := start + catsPerPageSel
	if end > len(cats) {
		end = len(cats)
	}
	var pageCats []categoryItem
	if start < len(cats) {
		pageCats = cats[start:end]
	}

	for i := 0; i < catsPerPageSel; i++ {
		path := filepath.Join(polymarketTmpDir, fmt.Sprintf("cat-%d.png", i))
		if i < len(pageCats) {
			c := pageCats[i]
			saveCatCard(path, c.Name, c.Name == selectedName, c.Visits, c.Count)
		} else {
			renderPolymarketEmptyAt(path)
		}
	}

	// Page indicator
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	pageText := fmt.Sprintf("%d/%d", catPage+1, totalPages)
	faceLG := getFontFace(fontDejaVuSansBold, 14)
	tw, th := measureText(faceLG, pageText)
	drawTextTopLeft(img, (keySize-tw)/2, (keySize-th)/2, pageText, color.RGBA{R: 100, G: 100, B: 140, A: 255}, faceLG)
	_ = savePNGAtomic(filepath.Join(polymarketTmpDir, "cat-page.png"), img)
}

func saveCatCard(path, name string, isSelected bool, visitCount, betCount int) {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	if isSelected {
		drawGradientV(img, 0, 0, keySize, keySize,
			color.RGBA{R: 20, G: 30, B: 60, A: 255},
			color.RGBA{R: 45, G: 50, B: 80, A: 255},
		)
		out := color.RGBA{R: 80, G: 160, B: 255, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 8, nil, &out, 3)
	} else {
		drawGradientV(img, 0, 0, keySize, keySize,
			color.RGBA{R: 18, G: 18, B: 28, A: 255},
			color.RGBA{R: 10, G: 10, B: 16, A: 255},
		)
		out := color.RGBA{R: 50, G: 50, B: 70, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 8, nil, &out, 1)
	}

	col := color.RGBA{R: 200, G: 200, B: 220, A: 255}
	if isSelected {
		col = color.RGBA{R: 120, G: 200, B: 255, A: 255}
	}

	maxW := keySize - 12
	type fontOpt struct {
		size  float64
		lineH int
	}
	opts := []fontOpt{{14, 16}, {12, 14}}
	chosenFace := getFontFace(fontDejaVuSansBold, 12)
	chosenLineH := 14
	var chosenLines []string

	for _, opt := range opts {
		face := getFontFace(fontDejaVuSansBold, opt.size)
		words := strings.Fields(name)
		var lines []string
		current := ""
		for _, w := range words {
			test := w
			if current != "" {
				test = current + " " + w
			}
			tw, _ := measureText(face, test)
			if tw > maxW && current != "" {
				lines = append(lines, current)
				current = w
			} else {
				current = test
			}
		}
		if current != "" {
			lines = append(lines, current)
		}
		fits := true
		for _, l := range lines {
			tw, _ := measureText(face, l)
			if tw > maxW {
				fits = false
				break
			}
		}
		chosenFace = face
		chosenLineH = opt.lineH
		chosenLines = lines
		if fits {
			break
		}
	}

	totalH := len(chosenLines) * chosenLineH
	yStart := (keySize-totalH)/2 - 2
	if betCount > 0 {
		yStart -= 6
	}
	for i, line := range chosenLines {
		tw, _ := measureText(chosenFace, line)
		drawTextTopLeft(img, (keySize-tw)/2, yStart+i*chosenLineH, line, col, chosenFace)
	}

	faceXS := getFontFace(fontDejaVuSans, 9)
	if betCount > 0 {
		vtext := fmt.Sprintf("%d bets", betCount)
		tw3, _ := measureText(faceXS, vtext)
		drawTextTopLeft(img, (keySize-tw3)/2, 56, vtext, color.RGBA{R: 100, G: 100, B: 130, A: 255}, faceXS)
	}
	if isSelected {
		fillCircle(img, 11, 11, 5, color.RGBA{R: 80, G: 200, B: 100, A: 255})
	}
	if visitCount > 0 {
		vtext := fmt.Sprintf("x%d", visitCount)
		tw4, _ := measureText(faceXS, vtext)
		drawTextTopLeft(img, keySize-tw4-6, 4, vtext, color.RGBA{R: 140, G: 140, B: 100, A: 255}, faceXS)
	}

	_ = savePNGAtomic(path, img)
}

func polymarketInit() {
	s := loadPolymarketState()
	s.Loading = true
	savePolymarketState(s)
	clearOldPolymarketImages()

	_, query, tagID := getPolymarketCategory(s)
	markets := fetchMarkets(false, query, tagID)
	displayPolymarketPage(&s, markets)

	s.Loading = false
	savePolymarketState(s)
}

func polymarketNext() {
	s := loadPolymarketState()
	s.Loading = true
	savePolymarketState(s)
	clearOldPolymarketImages()

	_, query, tagID := getPolymarketCategory(s)
	markets := fetchMarkets(false, query, tagID)
	totalPages := (len(markets) + betsPerPage - 1) / betsPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	s.Page++
	if s.Page >= totalPages {
		s.Page = totalPages - 1
	}
	displayPolymarketPage(&s, markets)

	s.Loading = false
	savePolymarketState(s)
}

func polymarketPrev() {
	s := loadPolymarketState()
	s.Loading = true
	savePolymarketState(s)
	clearOldPolymarketImages()

	_, query, tagID := getPolymarketCategory(s)
	markets := fetchMarkets(false, query, tagID)
	s.Page--
	if s.Page < 0 {
		s.Page = 0
	}
	displayPolymarketPage(&s, markets)

	s.Loading = false
	savePolymarketState(s)
}

func polymarketReload() {
	s := loadPolymarketState()
	s.Page = 0
	s.Loading = true
	savePolymarketState(s)
	clearOldPolymarketImages()

	buildCategories(s, true)
	_, query, tagID := getPolymarketCategory(s)
	markets := fetchMarkets(true, query, tagID)
	displayPolymarketPage(&s, markets)

	s.Loading = false
	savePolymarketState(s)
}

func polymarketMore(slot int) {
	if slot < 0 || slot >= betsPerPage {
		return
	}
	s := loadPolymarketState()
	_, query, tagID := getPolymarketCategory(s)
	markets := fetchMarkets(false, query, tagID)
	if len(markets) == 0 {
		return
	}
	idx := s.Page*betsPerPage + slot
	if idx >= len(markets) {
		return
	}
	for len(s.Frames) < 3 {
		s.Frames = append(s.Frames, 0)
	}

	allLines := wrapWords(markets[idx].Question, textGridCols)
	maxScroll := len(allLines) - textGridRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	totalSteps := 1
	if maxScroll > 0 {
		totalSteps = int(math.Ceil(float64(maxScroll)/float64(textScrollStep))) + 1
	}

	if s.Frames[slot] >= totalSteps-1 {
		s.Frames[slot] = 0
	} else {
		s.Frames[slot]++
	}
	savePolymarketState(s)
	renderPolymarketTextSlot(&markets[idx], slot, s.Frames[slot])
}

func polymarketRenderTextCmd(slot int) string {
	emptyPath := resolveAsset("decks/assets/empty.png")
	if slot < 0 || slot >= betsPerPage {
		return emptyPath
	}
	s := loadPolymarketState()
	if s.Loading {
		return emptyPath
	}
	cPath := marketCachePath(s.CatQuery, s.CatTagID)
	data, err := os.ReadFile(cPath)
	if err != nil {
		return emptyPath
	}
	var cache marketCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return emptyPath
	}
	idx := s.Page*betsPerPage + slot
	if idx < 0 || idx >= len(cache.Markets) {
		return emptyPath
	}
	frame := 0
	if slot < len(s.Frames) {
		frame = s.Frames[slot]
	}
	return renderPolymarketTextSlot(&cache.Markets[idx], slot, frame)
}

func polymarketCatInit() {
	s := loadPolymarketState()
	buildCategories(s, false)
	savePolymarketState(s)
	renderAllCatIcons()
}

func polymarketSelectCat(slot int) {
	if slot < 0 || slot >= catsPerPageSel {
		return
	}
	s := loadPolymarketState()
	data, err := os.ReadFile(polymarketCatsFile())
	if err != nil {
		return
	}
	var cats []categoryItem
	if err := json.Unmarshal(data, &cats); err != nil || len(cats) == 0 {
		return
	}

	totalPages := (len(cats) + catsPerPageSel - 1) / catsPerPageSel
	if totalPages < 1 {
		totalPages = 1
	}
	catPage := s.CatPage % totalPages
	if catPage < 0 {
		catPage += totalPages
	}
	start := catPage * catsPerPageSel
	idx := start + slot
	if idx >= len(cats) {
		return
	}

	cat := cats[idx]
	s.CatName = cat.Name
	s.CatQuery = cat.Query
	s.CatTagID = cat.TagID
	s.Page = 0
	s.Frames = []int{0, 0, 0}
	if s.CatVisits == nil {
		s.CatVisits = map[string]int{}
	}
	s.CatVisits[cat.Name]++
	savePolymarketState(s)

	updateCatsVisits(s)

	// Pre-render bet page for immediate display when returning to polymarket.deck
	markets := fetchMarkets(false, s.CatQuery, s.CatTagID)
	displayPolymarketPage(&s, markets)
	savePolymarketState(s)
}

func polymarketCatNext() {
	s := loadPolymarketState()
	if data, err := os.ReadFile(polymarketCatsFile()); err == nil {
		var cats []categoryItem
		if json.Unmarshal(data, &cats) == nil && len(cats) > 0 {
			totalPages := (len(cats) + catsPerPageSel - 1) / catsPerPageSel
			if s.CatPage < totalPages-1 {
				s.CatPage++
			}
		}
	}
	savePolymarketState(s)
	renderAllCatIcons()
}

func polymarketCatPrev() {
	s := loadPolymarketState()
	if s.CatPage > 0 {
		s.CatPage--
	}
	savePolymarketState(s)
	renderAllCatIcons()
}

func polymarketCatHome() {
	s := loadPolymarketState()
	s.CatPage = 0
	savePolymarketState(s)
	renderAllCatIcons()
}

func polymarketResetCatPage() {
	s := loadPolymarketState()
	s.CatPage = 0
	savePolymarketState(s)
}

// RunPolymarket executes the polymarket plugin CLI:
//
//	polymarket {init|next|prev|reload|more <0|1|2>|render-text <0|1|2>|cat-init|cat-prev|cat-next|cat-home|select-cat <0-8>|reset-cat-page}
func RunPolymarket(args []string) int {
	if len(args) < 1 {
		fmt.Println("Usage: deckctl polymarket {init|next|prev|reload|more <0|1|2>|render-text <0|1|2>|cat-init|cat-prev|cat-next|cat-home|select-cat <0-8>|reset-cat-page}")
		return 1
	}

	switch args[0] {
	case "init":
		polymarketInit()
	case "next":
		polymarketNext()
	case "prev":
		polymarketPrev()
	case "reload":
		polymarketReload()
	case "more":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl polymarket more <0|1|2>")
			return 1
		}
		slot, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid slot: %s\n", args[1])
			return 1
		}
		polymarketMore(slot)
	case "render-text":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl polymarket render-text <0|1|2>")
			return 1
		}
		slot, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid slot: %s\n", args[1])
			return 1
		}
		fmt.Println(polymarketRenderTextCmd(slot))
	case "cat-init":
		polymarketCatInit()
	case "cat-next":
		polymarketCatNext()
	case "cat-prev":
		polymarketCatPrev()
	case "cat-home":
		polymarketCatHome()
	case "select-cat":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl polymarket select-cat <0-8>")
			return 1
		}
		slot, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid slot: %s\n", args[1])
			return 1
		}
		polymarketSelectCat(slot)
	case "reset-cat-page":
		polymarketResetCatPage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown: %s\n", args[0])
		return 1
	}
	return 0
}
