package appplugins

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var (
	slotsStateFile = "/tmp/streamdeck-slots.json"
	slotsTmpDir    = "/tmp/streamdeck-slots"
	slotsSleep     = time.Sleep
)

const (
	slotsKeyCredits = 0
	slotsKeyLines   = 14 // display-only key; never touch action keys 4, 5, 9, 10
)

var (
	slotsSymbols = []string{"cherry", "lemon", "bell", "diamond", "seven", "star", "bar"}
	slotsWeights = []int{20, 20, 15, 10, 5, 15, 15}
	slotsPayouts = map[string]int{
		"cherry":  5,
		"lemon":   5,
		"bell":    10,
		"star":    15,
		"bar":     20,
		"diamond": 40,
		"seven":   77,
	}
	slotsBets = []int{1, 2, 5, 10, 25}
)

type gridPos struct {
	r, c int
}

var slotsGridKeys = map[gridPos]int{
	{0, 0}: 1, {0, 1}: 2, {0, 2}: 3,
	{1, 0}: 6, {1, 1}: 7, {1, 2}: 8,
	{2, 0}: 11, {2, 1}: 12, {2, 2}: 13,
}

var slotsWinLines = [][]gridPos{
	{{0, 0}, {0, 1}, {0, 2}},
	{{1, 0}, {1, 1}, {1, 2}},
	{{2, 0}, {2, 1}, {2, 2}},
	{{0, 0}, {1, 1}, {2, 2}},
	{{2, 0}, {1, 1}, {0, 2}},
}

type slotsState struct {
	Credits  int        `json:"credits"`
	Bet      int        `json:"bet"`
	Grid     [][]string `json:"grid"`
	Spinning bool       `json:"spinning"`
}

func defaultSlotsState() slotsState {
	return slotsState{
		Credits:  100,
		Bet:      5,
		Grid:     nil,
		Spinning: false,
	}
}

func loadSlotsState() slotsState {
	data, err := os.ReadFile(slotsStateFile)
	if err != nil {
		return defaultSlotsState()
	}
	var s slotsState
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultSlotsState()
	}
	if s.Bet <= 0 {
		s.Bet = 5
	}
	return s
}

func saveSlotsState(s slotsState) {
	if err := writeJSONAtomic(slotsStateFile, s); err != nil {
		fmt.Fprintf(os.Stderr, "slots save state error: %v\n", err)
	}
}

func slotsAssetPath(name string) string {
	return resolveAsset(filepath.Join("decks/slots/assets", name))
}

func pickSymbol(rng *rand.Rand) string {
	total := 0
	for _, w := range slotsWeights {
		total += w
	}
	r := rng.Intn(total)
	cum := 0
	for i, w := range slotsWeights {
		cum += w
		if r < cum {
			return slotsSymbols[i]
		}
	}
	return slotsSymbols[0]
}

func renderCreditsImage(credits int) string {
	img := newRGBA(color.RGBA{R: 10, G: 10, B: 30, A: 255})
	out := color.RGBA{R: 60, G: 60, B: 120, A: 255}
	drawRoundedRect(img, 2, 2, 70, 70, 6, nil, &out, 2)

	drawTextTopLeft(img, 12, 8, "CREDIT", color.RGBA{R: 120, G: 120, B: 200, A: 255}, getFontFace(fontDejaVuSansBold, 11))

	text := strconv.Itoa(credits)
	size := 28.0
	if len(text) > 3 {
		size = 18.0
	}
	face := getFontFace(fontDejaVuSansBold, size)
	tw, _ := measureText(face, text)
	drawTextTopLeft(img, (keySize-tw)/2, 30, text, color.RGBA{R: 255, G: 255, B: 100, A: 255}, face)

	path := filepath.Join(slotsTmpDir, "credits.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderBetDisplayImage(bet int) string {
	img := newRGBA(color.RGBA{R: 10, G: 10, B: 30, A: 255})
	out := color.RGBA{R: 100, G: 60, B: 60, A: 255}
	drawRoundedRect(img, 2, 2, 70, 70, 6, nil, &out, 2)

	drawTextTopLeft(img, 10, 6, "BET ▲", color.RGBA{R: 200, G: 120, B: 120, A: 255}, getFontFace(fontDejaVuSansBold, 11))

	text := strconv.Itoa(bet)
	face := getFontFace(fontDejaVuSansBold, 28)
	tw, _ := measureText(face, text)
	drawTextTopLeft(img, (keySize-tw)/2, 30, text, color.RGBA{R: 255, G: 150, B: 80, A: 255}, face)

	path := filepath.Join(slotsTmpDir, "bet-display.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderWinImage(amount int) string {
	img := newRGBA(color.RGBA{R: 10, G: 10, B: 30, A: 255})
	if amount > 0 {
		out := color.RGBA{R: 255, G: 215, B: 0, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 6, nil, &out, 3)
		drawTextTopLeft(img, 16, 6, "WIN!", color.RGBA{R: 255, G: 215, B: 0, A: 255}, getFontFace(fontDejaVuSansBold, 11))
		text := fmt.Sprintf("+%d", amount)
		size := 18.0
		if len(text) > 4 {
			size = 14.0
		}
		face := getFontFace(fontDejaVuSansBold, size)
		tw, _ := measureText(face, text)
		drawTextTopLeft(img, (keySize-tw)/2, 32, text, color.RGBA{R: 100, G: 255, B: 100, A: 255}, face)
	} else {
		out := color.RGBA{R: 40, G: 40, B: 60, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 6, nil, &out, 2)
		drawTextTopLeft(img, 10, 8, "LINES", color.RGBA{R: 120, G: 120, B: 160, A: 255}, getFontFace(fontDejaVuSansBold, 11))
		drawTextTopLeft(img, 24, 30, "5", color.RGBA{R: 180, G: 180, B: 255, A: 255}, getFontFace(fontDejaVuSansBold, 28))
	}

	path := filepath.Join(slotsTmpDir, fmt.Sprintf("win-%d.png", amount))
	_ = savePNGAtomic(path, img)
	return path
}

func renderSpinningFrame(col, frameIdx int) string {
	img := newRGBA(color.RGBA{R: 15, G: 15, B: 25, A: 255})
	rng := rand.New(rand.NewSource(int64(frameIdx*31 + col*7)))
	streakColors := []color.RGBA{
		{R: 220, G: 20, B: 20},
		{R: 255, G: 220, B: 40},
		{R: 60, G: 140, B: 255},
		{R: 255, G: 215, B: 0},
		{R: 0, G: 200, B: 50},
		{R: 180, G: 0, B: 220},
		{R: 255, G: 150, B: 80},
	}

	for y := 0; y < keySize; y += 4 {
		c := streakColors[rng.Intn(len(streakColors))]
		c.A = uint8(60 + rng.Intn(121))
		w := 20 + rng.Intn(37)
		xStart := 4 + rng.Intn(keySize-w-8)
		drawRoundedRect(img, xStart, y, xStart+w, y+3, 1, &c, nil, 0)
	}
	out := color.RGBA{R: 80, G: 80, B: 100, A: 120}
	drawRoundedRect(img, 1, 1, 71, 71, 3, nil, &out, 1)

	path := filepath.Join(slotsTmpDir, fmt.Sprintf("spinning-%d-%d.png", col, frameIdx))
	_ = savePNGAtomic(path, img)
	return path
}

func renderGlitterOverlay(symbol string, frameIdx int) string {
	symPath := slotsAssetPath(symbol + ".png")
	base, err := loadPNG(symPath)
	if err != nil {
		base = newRGBA(color.RGBA{R: 20, G: 20, B: 30, A: 255})
	}

	rng := rand.New(rand.NewSource(int64(frameIdx * 137)))
	sparkleColors := []color.RGBA{
		{R: 255, G: 255, B: 100}, {R: 255, G: 215, B: 0}, {R: 255, G: 255, B: 255},
		{R: 255, G: 180, B: 50}, {R: 200, G: 255, B: 200}, {R: 180, G: 220, B: 255},
		{R: 255, G: 150, B: 150}, {R: 150, G: 255, B: 150}, {R: 150, G: 150, B: 255},
	}
	for i := 0; i < 25; i++ {
		x := rng.Intn(keySize)
		y := rng.Intn(keySize)
		r := 1 + rng.Intn(3)
		c := sparkleColors[rng.Intn(len(sparkleColors))]
		c.A = uint8(120 + rng.Intn(136))
		fillCircle(base, x, y, r, c)
	}

	borderAlpha := 180 + int(75*(0.5+0.5*float64(frameIdx%3-1)))
	if borderAlpha < 0 {
		borderAlpha = 0
	}
	if borderAlpha > 255 {
		borderAlpha = 255
	}
	out := color.RGBA{R: 255, G: 215, B: 0, A: uint8(borderAlpha)}
	drawRoundedRect(base, 1, 1, 71, 71, 4, nil, &out, 3)

	path := filepath.Join(slotsTmpDir, fmt.Sprintf("glitter-%d-%s.png", frameIdx, symbol))
	_ = savePNGAtomic(path, base)
	return path
}

func renderBigWinFrame(amount, frameIdx int) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	pulse := 0.5 + 0.5*(float64(frameIdx%4)/3.0)
	fill := color.RGBA{R: uint8(80 * pulse), G: uint8(60 * pulse), B: 0, A: 255}
	out := color.RGBA{R: 255, G: 215, B: 0, A: 255}
	drawRoundedRect(img, 2, 2, 70, 70, 8, &fill, &out, 3)

	drawTextTopLeft(img, 14, 6, "WIN!", color.RGBA{R: 255, G: 255, B: 100, A: 255}, getFontFace(fontDejaVuSansBold, 14))

	text := fmt.Sprintf("+%d", amount)
	size := 18.0
	if len(text) > 4 {
		size = 14.0
	}
	face := getFontFace(fontDejaVuSansBold, size)
	tw, _ := measureText(face, text)
	yOff := 4 * (frameIdx % 2)
	drawTextTopLeft(img, (keySize-tw)/2, 30+yOff, text, color.RGBA{R: 100, G: 255, B: 100, A: 255}, face)

	rng := rand.New(rand.NewSource(int64(frameIdx * 99)))
	sparkCols := []color.RGBA{
		{R: 255, G: 255, B: 255, A: 255},
		{R: 255, G: 215, B: 0, A: 255},
		{R: 255, G: 255, B: 150, A: 255},
	}
	for i := 0; i < 10; i++ {
		x := 5 + rng.Intn(62)
		y := 5 + rng.Intn(62)
		fillCircle(img, x, y, 1, sparkCols[rng.Intn(len(sparkCols))])
	}

	path := filepath.Join(slotsTmpDir, fmt.Sprintf("bigwin-%d.png", frameIdx))
	_ = savePNGAtomic(path, img)
	return path
}

func checkSlotsWins(grid [][]string, bet int) (int, map[gridPos]bool) {
	total := 0
	winners := map[gridPos]bool{}
	if len(grid) < 3 || len(grid[0]) < 3 || len(grid[1]) < 3 || len(grid[2]) < 3 {
		return 0, winners
	}

	for _, line := range slotsWinLines {
		s0 := grid[line[0].r][line[0].c]
		s1 := grid[line[1].r][line[1].c]
		s2 := grid[line[2].r][line[2].c]
		if s0 == s1 && s1 == s2 {
			p := slotsPayouts[s0]
			if p == 0 {
				p = 5
			}
			total += p * bet
			for _, pos := range line {
				winners[pos] = true
			}
		} else if s0 == s1 || s1 == s2 {
			matching := s0
			if s0 != s1 {
				matching = s1
			}
			base := slotsPayouts[matching]
			p2 := base / 3
			if p2 < 1 {
				p2 = 1
			}
			total += p2 * bet
			if s0 == s1 {
				winners[line[0]] = true
				winners[line[1]] = true
			} else {
				winners[line[1]] = true
				winners[line[2]] = true
			}
		}
	}
	return total, winners
}

func showSlotsUI(s slotsState) {
	pushIcon(slotsKeyCredits, renderCreditsImage(s.Credits))
	pushIcon(slotsKeyLines, renderWinImage(0))
	renderBetDisplayImage(s.Bet)
}

func showSlotsGrid(grid [][]string) {
	for pos, key := range slotsGridKeys {
		sym := grid[pos.r][pos.c]
		pushIcon(key, slotsAssetPath(sym+".png"))
	}
}

func animateSlotsSpin(s *slotsState) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	grid := make([][]string, 3)
	for r := 0; r < 3; r++ {
		grid[r] = make([]string, 3)
		for c := 0; c < 3; c++ {
			grid[r][c] = pickSymbol(rng)
		}
	}

	bet := s.Bet
	s.Credits -= bet
	saveSlotsState(*s)
	pushIcon(slotsKeyCredits, renderCreditsImage(s.Credits))

	// Phase 1: All columns spinning
	for frame := 0; frame < 8; frame++ {
		for col := 0; col < 3; col++ {
			p := renderSpinningFrame(col, frame)
			for row := 0; row < 3; row++ {
				pushIcon(slotsGridKeys[gridPos{row, col}], p)
			}
		}
		slotsSleep(80 * time.Millisecond)
	}

	// Phase 2: Column 0 slows and stops
	for frame := 0; frame < 4; frame++ {
		p := renderSpinningFrame(0, frame+20)
		for row := 0; row < 3; row++ {
			pushIcon(slotsGridKeys[gridPos{row, 0}], p)
		}
		slotsSleep(120 * time.Millisecond)
	}
	for row := 0; row < 3; row++ {
		pushIcon(slotsGridKeys[gridPos{row, 0}], slotsAssetPath(grid[row][0]+".png"))
	}
	slotsSleep(50 * time.Millisecond)

	// Column 1 & 2 spin, then column 1 stops
	for frame := 0; frame < 3; frame++ {
		p1 := renderSpinningFrame(1, frame+30)
		p2 := renderSpinningFrame(2, frame+40)
		for row := 0; row < 3; row++ {
			pushIcon(slotsGridKeys[gridPos{row, 1}], p1)
			pushIcon(slotsGridKeys[gridPos{row, 2}], p2)
		}
		slotsSleep(120 * time.Millisecond)
	}
	for row := 0; row < 3; row++ {
		pushIcon(slotsGridKeys[gridPos{row, 1}], slotsAssetPath(grid[row][1]+".png"))
	}
	slotsSleep(50 * time.Millisecond)

	// Column 2 dramatic slowdown
	for frame := 0; frame < 4; frame++ {
		p2 := renderSpinningFrame(2, frame+50)
		for row := 0; row < 3; row++ {
			pushIcon(slotsGridKeys[gridPos{row, 2}], p2)
		}
		slotsSleep(time.Duration(150+frame*50) * time.Millisecond)
	}
	for row := 0; row < 3; row++ {
		pushIcon(slotsGridKeys[gridPos{row, 2}], slotsAssetPath(grid[row][2]+".png"))
	}
	slotsSleep(150 * time.Millisecond)

	winnings, winners := checkSlotsWins(grid, bet)
	if winnings > 0 {
		s.Credits += winnings
		for cycle := 0; cycle < 12; cycle++ {
			for pos := range winners {
				sym := grid[pos.r][pos.c]
				pushIcon(slotsGridKeys[pos], renderGlitterOverlay(sym, cycle))
			}
			pushIcon(slotsKeyLines, renderBigWinFrame(winnings, cycle))
			if cycle == 4 {
				pushIcon(slotsKeyCredits, renderCreditsImage(s.Credits))
			}
			slotsSleep(120 * time.Millisecond)
		}
		for pos := range winners {
			sym := grid[pos.r][pos.c]
			pushIcon(slotsGridKeys[pos], slotsAssetPath(sym+"-win.png"))
		}
		pushIcon(slotsKeyLines, renderWinImage(winnings))

		// Jackpot: all 9 same symbol
		first := grid[0][0]
		allSame := true
		for r := 0; r < 3 && allSame; r++ {
			for c := 0; c < 3; c++ {
				if grid[r][c] != first {
					allSame = false
					break
				}
			}
		}
		if allSame {
			for cycle := 0; cycle < 8; cycle++ {
				for _, key := range slotsGridKeys {
					pushIcon(key, renderGlitterOverlay(first, cycle+50))
				}
				pushIcon(slotsKeyLines, renderBigWinFrame(winnings, cycle+20))
				slotsSleep(150 * time.Millisecond)
			}
		}
	} else {
		pushIcon(slotsKeyLines, renderWinImage(0))
	}

	s.Grid = grid
	s.Spinning = false
	saveSlotsState(*s)
	pushIcon(slotsKeyCredits, renderCreditsImage(s.Credits))
}

func slotsInit() {
	s := loadSlotsState()
	if s.Credits <= 0 {
		s.Credits = 100
		s.Bet = 5
	}
	showSlotsUI(s)
	if len(s.Grid) == 3 {
		showSlotsGrid(s.Grid)
	} else {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		grid := make([][]string, 3)
		for r := 0; r < 3; r++ {
			grid[r] = make([]string, 3)
			for c := 0; c < 3; c++ {
				grid[r][c] = pickSymbol(rng)
			}
		}
		s.Grid = grid
		saveSlotsState(s)
		showSlotsGrid(grid)
	}
}

func slotsSpin() {
	s := loadSlotsState()
	if s.Credits <= 0 {
		s.Credits = 100
		s.Bet = 5
		s.Spinning = false
		saveSlotsState(s)
		showSlotsUI(s)
		return
	}
	if s.Spinning {
		return
	}
	if s.Credits < s.Bet {
		pushText(slotsKeyCredits, "LOW!", "#ff4444", "#1a0000", 14)
		slotsSleep(1 * time.Second)
		pushIcon(slotsKeyCredits, renderCreditsImage(s.Credits))
		return
	}
	s.Spinning = true
	saveSlotsState(s)
	animateSlotsSpin(&s)
}

func slotsBetUp() {
	s := loadSlotsState()
	idx := 2
	for i, b := range slotsBets {
		if b == s.Bet {
			idx = i
			break
		}
	}
	if idx < len(slotsBets)-1 {
		idx++
	}
	s.Bet = slotsBets[idx]
	saveSlotsState(s)
	renderBetDisplayImage(s.Bet)
}

func slotsBetDown() {
	s := loadSlotsState()
	idx := 2
	for i, b := range slotsBets {
		if b == s.Bet {
			idx = i
			break
		}
	}
	if idx > 0 {
		idx--
	}
	s.Bet = slotsBets[idx]
	saveSlotsState(s)
	renderBetDisplayImage(s.Bet)
}

// RunSlots executes the slot machine plugin CLI:
//
//	slots {init|spin|bet-up|bet-down|bet-icon}
func RunSlots(args []string) int {
	if len(args) < 1 {
		fmt.Println("Usage: deckctl slots {init|spin|bet-up|bet-down|bet-icon}")
		return 1
	}

	switch args[0] {
	case "init":
		slotsInit()
	case "spin":
		slotsSpin()
	case "bet-up":
		slotsBetUp()
	case "bet-down":
		slotsBetDown()
	case "bet-icon":
		s := loadSlotsState()
		fmt.Println(renderBetDisplayImage(s.Bet))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		return 1
	}
	return 0
}
