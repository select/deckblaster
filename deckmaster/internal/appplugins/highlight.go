package appplugins

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var (
	highlightStateFile = expandHome("~/.local/share/streamdeck-highlight.json")
	highlightPIDFile   = "/tmp/streamdeck-highlight.pid"
	highlightTmpDir    = "/tmp/streamdeck-highlight"
	highlightWalFile   = expandHome("~/.cache/wal/colors.json")
)

const (
	highlightKeyStatus = 0
	hatiSchema         = "org.gnome.shell.extensions.hati"
	hatiUUID           = "hati@szymonwilczek.github.io"
)

var (
	highlightRadii     = []int{10, 15, 20, 30, 40, 50, 70}
	highlightOpacities = []float64{0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
	hatiSizes          = []int{40, 50, 60, 80, 100, 130, 160}
)

type walColor struct {
	Hex  string
	Name string
}

var walFallback = []walColor{
	{"#95281D", "DkRed"},
	{"#DB601B", "Orange"},
	{"#AF414D", "Rose"},
	{"#F39E21", "Amber"},
	{"#18608F", "Blue"},
	{"#DC588C", "Pink"},
	{"#c2c1c5", "Grey"},
}

type highlightState struct {
	Active     bool   `json:"active"`
	RadiusIdx  int    `json:"radius_idx"`
	ColorIdx   int    `json:"color_idx"`
	OpacityIdx int    `json:"opacity_idx"`
	Mode       string `json:"mode,omitempty"`
}

func defaultHighlightState() highlightState {
	return highlightState{
		Active:     false,
		RadiusIdx:  2,
		ColorIdx:   0,
		OpacityIdx: 3,
	}
}

func loadHighlightState() highlightState {
	data, err := os.ReadFile(highlightStateFile)
	if err != nil {
		return defaultHighlightState()
	}
	var s highlightState
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultHighlightState()
	}
	return s
}

func saveHighlightState(s highlightState) {
	if err := writeJSONAtomic(highlightStateFile, s); err != nil {
		fmt.Fprintf(os.Stderr, "highlight save state error: %v\n", err)
	}
}

func loadWalColors() []walColor {
	data, err := os.ReadFile(highlightWalFile)
	if err != nil {
		return append([]walColor(nil), walFallback...)
	}
	var parsed struct {
		Colors map[string]string `json:"colors"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Colors) == 0 {
		return append([]walColor(nil), walFallback...)
	}
	names := []string{"DkRed", "Orange", "Rose", "Amber", "Blue", "Pink", "Grey"}
	out := make([]walColor, 0, 7)
	for i := 1; i <= 7; i++ {
		hexVal, ok := parsed.Colors[fmt.Sprintf("color%d", i)]
		if !ok || !strings.HasPrefix(hexVal, "#") || len(hexVal) < 7 {
			return append([]walColor(nil), walFallback...)
		}
		out = append(out, walColor{Hex: hexVal, Name: names[i-1]})
	}
	return out
}

func detectHighlightBackend() string {
	session := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
	if strings.Contains(session, "wayland") {
		return "wayland"
	}
	return "x11"
}

func hatiSchemaDir() string {
	return expandHome("~/.local/share/gnome-shell/extensions/hati@szymonwilczek.github.io/schemas/")
}

func hatiGsettingsGet(key string) string {
	cmd := exec.Command("gsettings", "get", hatiSchema, key)
	cmd.Env = append(os.Environ(), "GSETTINGS_SCHEMA_DIR="+hatiSchemaDir())
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hatiGsettingsSet(key, value string) {
	cmd := exec.Command("gsettings", "set", hatiSchema, key, value)
	cmd.Env = append(os.Environ(), "GSETTINGS_SCHEMA_DIR="+hatiSchemaDir())
	_ = cmd.Run()
}

func hatiIsEnabled() bool {
	return hatiGsettingsGet("enabled") == "true"
}

func hatiEnable() {
	_ = exec.Command("gnome-extensions", "enable", hatiUUID).Run()
	hatiGsettingsSet("enabled", "true")
}

func hatiDisable() {
	hatiGsettingsSet("enabled", "false")
}

func clampIdx(idx, length int) int {
	if length <= 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= length {
		return length - 1
	}
	return idx
}

func modIdx(idx, length int) int {
	if length <= 0 {
		return 0
	}
	m := idx % length
	if m < 0 {
		m += length
	}
	return m
}

func hatiApplySettings(s highlightState) {
	colors := loadWalColors()
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	opacity := highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))]
	hatiSize := hatiSizes[clampIdx(s.RadiusIdx, len(hatiSizes))]
	mode := s.Mode
	if mode == "" {
		mode = "ring+click"
	}

	hatiGsettingsSet("shape", "'circle'")
	hatiGsettingsSet("corner-radius", "50")

	rgbaStr := fmt.Sprintf("rgba(%d, %d, %d, %.1f)", c.R, c.G, c.B, opacity)
	if mode == "click-only" {
		hatiGsettingsSet("color", "'rgba(0, 0, 0, 0.0)'")
		hatiGsettingsSet("opacity", "0.0")
		hatiGsettingsSet("glow", "false")
		hatiGsettingsSet("size", strconv.Itoa(hatiSize))
		hatiGsettingsSet("border-weight", "6")
	} else {
		hatiGsettingsSet("color", fmt.Sprintf("'%s'", rgbaStr))
		hatiGsettingsSet("size", strconv.Itoa(hatiSize))
		hatiGsettingsSet("opacity", fmt.Sprintf("%g", opacity))
		hatiGsettingsSet("glow", "true")
		hatiGsettingsSet("border-weight", "4")
	}

	clickRGBA := fmt.Sprintf("rgba(%d, %d, %d, 1.0)", c.R, c.G, c.B)
	hatiGsettingsSet("click-animations", "true")
	if mode == "click-only" {
		hatiGsettingsSet("click-animation-mode", "'ring-expand'")
	} else {
		hatiGsettingsSet("click-animation-mode", "'ripple'")
	}
	hatiGsettingsSet("left-click-color", fmt.Sprintf("'%s'", clickRGBA))
	hatiGsettingsSet("right-click-color", fmt.Sprintf("'rgba(%d, %d, %d, 0.7)'", c.R, c.G, c.B))
	_ = exec.Command("gsettings", "set", "org.gnome.desktop.interface", "locate-pointer", "false").Run()
}

func isHighlightRunning(backend string) bool {
	if backend == "wayland" {
		return hatiIsEnabled()
	}
	data, err := os.ReadFile(highlightPIDFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = os.Remove(highlightPIDFile)
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		_ = os.Remove(highlightPIDFile)
		return false
	}
	return true
}

func startHighlight(backend string, s *highlightState) {
	if backend == "wayland" {
		hatiApplySettings(*s)
		hatiEnable()
	} else {
		colors := loadWalColors()
		colHex := colors[modIdx(s.ColorIdx, len(colors))].Hex
		opacity := highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))]
		radius := highlightRadii[clampIdx(s.RadiusIdx, len(highlightRadii))]
		bin := resolveAsset("decks/highlight/highlight-pointer")

		cmd := exec.Command(bin,
			"--released-color", colHex,
			"--pressed-color", "#ffffff",
			"--radius", strconv.Itoa(radius),
			"--opacity", fmt.Sprintf("%g", opacity),
			"--outline", "0",
			"--show-cursor",
		)
		cmd.Env = append(os.Environ(), "DISPLAY=:1")
		if err := cmd.Start(); err == nil && cmd.Process != nil {
			_ = writeFileAtomic(highlightPIDFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
			_ = cmd.Process.Release()
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "start highlight-pointer error: %v\n", err)
		}
	}
	s.Active = true
	saveHighlightState(*s)
}

func stopHighlight(backend string, s *highlightState) {
	if backend == "wayland" {
		hatiDisable()
	} else {
		if data, err := os.ReadFile(highlightPIDFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGTERM)
			}
			_ = os.Remove(highlightPIDFile)
		}
		_ = exec.Command("killall", "highlight-pointer").Run()
	}
	s.Active = false
	saveHighlightState(*s)
}

func renderHighlightStatus(s highlightState, backend string, running bool) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	active := s.Active && running
	var mode string
	if backend == "wayland" {
		mode = s.Mode
		if mode == "" {
			mode = "off"
		}
	}

	colors := loadWalColors()
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)

	if active {
		fill := color.RGBA{R: 15, G: 30, B: 15, A: 255}
		out := color.RGBA{R: 0, G: 180, B: 0, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 8, &fill, &out, 2)
	} else {
		fill := color.RGBA{R: 25, G: 10, B: 10, A: 255}
		out := color.RGBA{R: 120, G: 40, B: 40, A: 255}
		drawRoundedRect(img, 2, 2, 70, 70, 8, &fill, &out, 2)
	}

	var status string
	var statusCol color.RGBA
	if !active {
		status = "OFF"
		statusCol = color.RGBA{R: 255, G: 60, B: 60, A: 255}
	} else if mode == "click-only" {
		status = "CLICK"
		statusCol = color.RGBA{R: 100, G: 200, B: 255, A: 255}
	} else {
		status = "ON"
		statusCol = color.RGBA{R: 0, G: 255, B: 80, A: 255}
	}
	drawTextTopLeft(img, 20, 4, status, statusCol, getFontFace(fontDejaVuSansBold, 18))

	rIdx := clampIdx(s.RadiusIdx, len(highlightRadii))
	radiusVal := highlightRadii[rIdx]
	previewR := radiusVal / 2
	if previewR > 20 {
		previewR = 20
	}
	alpha := uint8(highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))] * 255)
	fillCircle(img, 36, 42, previewR, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})

	drawTextTopLeft(img, 8, 58, fmt.Sprintf("r=%d", radiusVal), color.RGBA{R: 180, G: 180, B: 180, A: 255}, getFontFace(fontDejaVuSansBold, 10))

	path := filepath.Join(highlightTmpDir, "status.png")
	if err := savePNGAtomic(path, img); err != nil {
		fmt.Fprintf(os.Stderr, "highlight status render error: %v\n", err)
	}
	return path
}

func renderHighlightBtnSizeUp(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	drawGradientV(img, 0, 0, keySize, keySize, color.RGBA{R: 25, G: 40, B: 55, A: 255}, color.RGBA{R: 12, G: 20, B: 30, A: 255})
	out := color.RGBA{R: 50, G: 100, B: 160, A: 255}
	drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)

	ridx := clampIdx(s.RadiusIdx, len(highlightRadii))
	atMax := ridx >= len(highlightRadii)-1
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	alpha := uint8(highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))] * 255)

	dotR := highlightRadii[ridx]/2 + 8
	if dotR < 16 {
		dotR = 16
	}
	if dotR > 28 {
		dotR = 28
	}
	if atMax {
		fillCircle(img, 36, 36, dotR, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha / 2})
		strokeCircle(img, 36, 36, dotR, 1, color.RGBA{R: 60, G: 60, B: 80, A: 255})
	} else {
		fillCircle(img, 36, 36, dotR, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
	}

	path := filepath.Join(highlightTmpDir, "btn-size-up.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnSizeDown(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	drawGradientV(img, 0, 0, keySize, keySize, color.RGBA{R: 25, G: 40, B: 55, A: 255}, color.RGBA{R: 12, G: 20, B: 30, A: 255})
	out := color.RGBA{R: 50, G: 100, B: 160, A: 255}
	drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)

	ridx := clampIdx(s.RadiusIdx, len(highlightRadii))
	atMin := ridx <= 0
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	alpha := uint8(highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))] * 255)

	dotR := highlightRadii[ridx] / 4
	if dotR < 4 {
		dotR = 4
	}
	if atMin {
		fillCircle(img, 36, 36, dotR, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha / 2})
		strokeCircle(img, 36, 36, dotR, 1, color.RGBA{R: 60, G: 60, B: 80, A: 255})
	} else {
		fillCircle(img, 36, 36, dotR, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
	}

	path := filepath.Join(highlightTmpDir, "btn-size-down.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnColorNext(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	nextIdx := modIdx(s.ColorIdx+1, len(colors))
	c := parseHexColor(colors[nextIdx].Hex)
	fillCircle(img, 36, 36, 24, c)

	path := filepath.Join(highlightTmpDir, "btn-color-next.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnColorPrev(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	prevIdx := modIdx(s.ColorIdx-1, len(colors))
	c := parseHexColor(colors[prevIdx].Hex)
	fillCircle(img, 36, 36, 24, c)

	path := filepath.Join(highlightTmpDir, "btn-color-prev.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnAlphaUp(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	drawGradientV(img, 0, 0, keySize, keySize, color.RGBA{R: 50, G: 45, B: 15, A: 255}, color.RGBA{R: 28, G: 25, B: 8, A: 255})
	out := color.RGBA{R: 180, G: 140, B: 30, A: 255}
	drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)

	oidx := clampIdx(s.OpacityIdx, len(highlightOpacities))
	atMax := oidx >= len(highlightOpacities)-1
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	nextIdx := clampIdx(oidx+1, len(highlightOpacities))
	nextOp := highlightOpacities[nextIdx]
	alpha := uint8(nextOp * 255)

	if atMax {
		fillCircle(img, 36, 32, 16, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha / 2})
		strokeCircle(img, 36, 32, 16, 1, color.RGBA{R: 60, G: 55, B: 30, A: 255})
	} else {
		fillCircle(img, 36, 32, 16, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
	}

	pct := fmt.Sprintf("%d%%", int(nextOp*100))
	face := getFontFace(fontDejaVuSansBold, 10)
	tw, _ := measureText(face, pct)
	lblCol := color.RGBA{R: 255, G: 230, B: 120, A: 255}
	if atMax {
		lblCol = color.RGBA{R: 60, G: 55, B: 30, A: 255}
	}
	drawTextTopLeft(img, (keySize-tw)/2, 54, pct, lblCol, face)

	path := filepath.Join(highlightTmpDir, "btn-alpha-up.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnAlphaDown(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	drawGradientV(img, 0, 0, keySize, keySize, color.RGBA{R: 50, G: 45, B: 15, A: 255}, color.RGBA{R: 28, G: 25, B: 8, A: 255})
	out := color.RGBA{R: 180, G: 140, B: 30, A: 255}
	drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)

	oidx := clampIdx(s.OpacityIdx, len(highlightOpacities))
	atMin := oidx <= 0
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	prevIdx := clampIdx(oidx-1, len(highlightOpacities))
	prevOp := highlightOpacities[prevIdx]
	alpha := uint8(prevOp * 255)

	if atMin {
		fillCircle(img, 36, 32, 16, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha / 2})
		strokeCircle(img, 36, 32, 16, 1, color.RGBA{R: 60, G: 55, B: 30, A: 255})
	} else {
		fillCircle(img, 36, 32, 16, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
	}

	pct := fmt.Sprintf("%d%%", int(prevOp*100))
	face := getFontFace(fontDejaVuSansBold, 10)
	tw, _ := measureText(face, pct)
	lblCol := color.RGBA{R: 255, G: 230, B: 120, A: 255}
	if atMin {
		lblCol = color.RGBA{R: 60, G: 55, B: 30, A: 255}
	}
	drawTextTopLeft(img, (keySize-tw)/2, 54, pct, lblCol, face)

	path := filepath.Join(highlightTmpDir, "btn-alpha-down.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderHighlightBtnMode(s highlightState, colors []walColor) string {
	img := newRGBA(color.RGBA{R: 0, G: 0, B: 0, A: 255})
	mode := s.Mode
	if mode == "" {
		mode = "ring+click"
	}
	c := parseHexColor(colors[modIdx(s.ColorIdx, len(colors))].Hex)
	alpha := uint8(highlightOpacities[clampIdx(s.OpacityIdx, len(highlightOpacities))] * 255)

	var label string
	var labelCol color.RGBA
	if mode == "ring+click" {
		out := color.RGBA{R: 0, G: 180, B: 0, A: 255}
		drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)
		strokeCircle(img, 36, 30, 18, 3, color.RGBA{R: c.R, G: c.G, B: c.B, A: alpha})
		fillCircle(img, 36, 30, 4, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		label = "RING"
		labelCol = color.RGBA{R: 0, G: 220, B: 100, A: 255}
	} else {
		out := color.RGBA{R: 80, G: 160, B: 255, A: 255}
		drawRoundedRect(img, 3, 3, 69, 69, 10, nil, &out, 2)
		fillCircle(img, 36, 30, 5, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		strokeCircle(img, 36, 30, 12, 1, color.RGBA{R: c.R, G: c.G, B: c.B, A: 120})
		strokeCircle(img, 36, 30, 20, 1, color.RGBA{R: c.R, G: c.G, B: c.B, A: 120})
		label = "CLICK"
		labelCol = color.RGBA{R: 100, G: 200, B: 255, A: 255}
	}

	face := getFontFace(fontDejaVuSansBold, 10)
	tw, _ := measureText(face, label)
	drawTextTopLeft(img, (keySize-tw)/2, 54, label, labelCol, face)

	path := filepath.Join(highlightTmpDir, "btn-mode.png")
	_ = savePNGAtomic(path, img)
	return path
}

func renderAllHighlightButtons(s highlightState) {
	colors := loadWalColors()
	renderHighlightBtnSizeUp(s, colors)
	renderHighlightBtnSizeDown(s, colors)
	renderHighlightBtnColorNext(s, colors)
	renderHighlightBtnColorPrev(s, colors)
	renderHighlightBtnAlphaUp(s, colors)
	renderHighlightBtnAlphaDown(s, colors)
	renderHighlightBtnMode(s, colors)
}

func renderHighlightBtnByType(btnType string, s highlightState) (string, bool) {
	colors := loadWalColors()
	switch btnType {
	case "size-up":
		return renderHighlightBtnSizeUp(s, colors), true
	case "size-down":
		return renderHighlightBtnSizeDown(s, colors), true
	case "color-next":
		return renderHighlightBtnColorNext(s, colors), true
	case "color-prev":
		return renderHighlightBtnColorPrev(s, colors), true
	case "alpha-up":
		return renderHighlightBtnAlphaUp(s, colors), true
	case "alpha-down":
		return renderHighlightBtnAlphaDown(s, colors), true
	case "mode":
		return renderHighlightBtnMode(s, colors), true
	default:
		return "", false
	}
}

func updateHighlightUI(s highlightState, backend string, running bool) {
	path := renderHighlightStatus(s, backend, running)
	pushIcon(highlightKeyStatus, path)
	renderAllHighlightButtons(s)
}

func applyHighlightChange(backend string, s *highlightState) {
	if isHighlightRunning(backend) {
		if backend == "wayland" {
			hatiApplySettings(*s)
		} else {
			stopHighlight(backend, s)
			startHighlight(backend, s)
		}
	}
}

// RunHighlight executes the mouse highlighter plugin CLI:
//
//	highlight {init|toggle|mode|size-up|size-down|color-next|color-prev|opacity-up|opacity-down|btn-icon <type>}
func RunHighlight(args []string) int {
	if len(args) < 1 {
		fmt.Println("Usage: deckctl highlight {init|toggle|mode|size-up|size-down|color-next|color-prev|opacity-up|opacity-down|btn-icon <type>}")
		return 1
	}

	backend := detectHighlightBackend()
	cmd := args[0]

	if cmd == "btn-icon" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: deckctl highlight btn-icon {size-up|size-down|color-next|color-prev|alpha-up|alpha-down|mode}")
			return 1
		}
		s := loadHighlightState()
		p, ok := renderHighlightBtnByType(args[1], s)
		if !ok {
			fmt.Fprintf(os.Stderr, "Unknown btn-icon type: %s\n", args[1])
			return 1
		}
		fmt.Println(p)
		return 0
	}

	s := loadHighlightState()
	switch cmd {
	case "init":
		running := isHighlightRunning(backend)
		s.Active = running
		saveHighlightState(s)
		updateHighlightUI(s, backend, running)
	case "toggle":
		if isHighlightRunning(backend) {
			stopHighlight(backend, &s)
			updateHighlightUI(s, backend, false)
		} else {
			startHighlight(backend, &s)
			updateHighlightUI(s, backend, true)
		}
	case "mode":
		if s.Mode == "" || s.Mode == "ring+click" {
			s.Mode = "click-only"
		} else {
			s.Mode = "ring+click"
		}
		saveHighlightState(s)
		running := isHighlightRunning(backend)
		if running && backend == "wayland" {
			hatiApplySettings(s)
		}
		updateHighlightUI(s, backend, running)
	case "size-up":
		s.RadiusIdx = clampIdx(s.RadiusIdx+1, len(highlightRadii))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	case "size-down":
		s.RadiusIdx = clampIdx(s.RadiusIdx-1, len(highlightRadii))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	case "color-next":
		colors := loadWalColors()
		s.ColorIdx = modIdx(s.ColorIdx+1, len(colors))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	case "color-prev":
		colors := loadWalColors()
		s.ColorIdx = modIdx(s.ColorIdx-1, len(colors))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	case "opacity-up":
		s.OpacityIdx = clampIdx(s.OpacityIdx+1, len(highlightOpacities))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	case "opacity-down":
		s.OpacityIdx = clampIdx(s.OpacityIdx-1, len(highlightOpacities))
		saveHighlightState(s)
		applyHighlightChange(backend, &s)
		updateHighlightUI(s, backend, isHighlightRunning(backend))
	default:
		fmt.Fprintf(os.Stderr, "Unknown: %s\n", cmd)
		return 1
	}
	return 0
}
