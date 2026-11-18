package sysplugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	haCacheDir        = "/tmp/streamdeck-ha"
	haStatesCacheFile = "/tmp/streamdeck-ha/states-cache.json"
	haStatesLockFile  = "/tmp/streamdeck-ha/states-cache.lock"
	haStatesCacheTTL  = 2500 * time.Millisecond
	haHTTPTimeout     = 5 * time.Second
)

type haEntityState struct {
	EntityID    string `json:"entity_id"`
	State       string `json:"state"`
	LastChanged string `json:"last_changed"`
}

type haStatesCache struct {
	FetchedAt time.Time                `json:"fetched_at"`
	States    map[string]haEntityState `json:"states"`
}

type haClient struct {
	baseURL string
	token   string
	deckAPI string
	http    *http.Client
	cache   string
}

func newHAClient() *haClient {
	return &haClient{
		baseURL: strings.TrimRight(getEnvOrConfig("HA_URL", "http://homeassistant.local:8123"), "/"),
		token:   getEnvOrConfig("HA_TOKEN", ""),
		deckAPI: strings.TrimRight(getEnvOrConfig("DECK_API", "http://localhost:9990"), "/"),
		http:    &http.Client{Timeout: haHTTPTimeout},
		cache:   haCacheDir,
	}
}

func (c *haClient) haGet(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HA GET %s: status %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *haClient) haPost(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HA POST %s: status %d", path, resp.StatusCode)
	}
	return nil
}

func (c *haClient) getStatesMap() (map[string]haEntityState, error) {
	if err := os.MkdirAll(c.cache, 0o755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(c.cache, "states-cache.lock")
	cachePath := filepath.Join(c.cache, "states-cache.json")

	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	defer lf.Close()

	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer func() { _ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) }()

	// Check existing cache
	if raw, err := os.ReadFile(cachePath); err == nil {
		var sc haStatesCache
		if json.Unmarshal(raw, &sc) == nil && len(sc.States) > 0 && time.Since(sc.FetchedAt) < haStatesCacheTTL {
			return sc.States, nil
		}
	}

	// Fetch all states in one batch request
	data, err := c.haGet("/api/states")
	if err != nil {
		return nil, err
	}
	var list []haEntityState
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	m := make(map[string]haEntityState, len(list))
	for _, st := range list {
		if st.EntityID != "" {
			m[st.EntityID] = st
		}
	}
	sc := haStatesCache{
		FetchedAt: time.Now().UTC(),
		States:    m,
	}
	if encoded, err := json.Marshal(sc); err == nil {
		_ = atomicWriteFile(cachePath, encoded, 0o644)
	}
	return m, nil
}

func (c *haClient) invalidateStatesCache() {
	_ = os.Remove(filepath.Join(c.cache, "states-cache.json"))
}

func (c *haClient) getEntityStateAndTime(entity string) (string, string) {
	if m, err := c.getStatesMap(); err == nil {
		if st, ok := m[entity]; ok {
			if st.State == "" {
				return "?", st.LastChanged
			}
			return st.State, st.LastChanged
		}
	}
	// Fallback to single-entity endpoint
	data, err := c.haGet("/api/states/" + entity)
	if err != nil {
		return "?", ""
	}
	var st haEntityState
	if err := json.Unmarshal(data, &st); err != nil || st.State == "" {
		return "?", ""
	}
	return st.State, st.LastChanged
}

func (c *haClient) getState(entity string) string {
	st, _ := c.getEntityStateAndTime(entity)
	return st
}

// visualSignatureMatches checks if <outPath>.sig equals sig and outPath exists.
func visualSignatureMatches(outPath, sig string) bool {
	if !fileExists(outPath) {
		return false
	}
	b, err := os.ReadFile(outPath + ".sig")
	if err != nil {
		return false
	}
	return string(b) == sig
}

func writeVisualSignature(outPath, sig string) {
	_ = atomicWriteFile(outPath+".sig", []byte(sig), 0o644)
}

func formatRelativeTime(timeStr string, now time.Time) string {
	if timeStr == "" {
		return "?"
	}
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, timeStr)
		if err != nil {
			return "?"
		}
	}
	secs := now.Sub(t).Seconds()
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return "<1m"
	case secs < 3600:
		return fmt.Sprintf("%dm", int(secs/60))
	case secs < 86400:
		return fmt.Sprintf("%dh", int(secs/3600))
	default:
		return fmt.Sprintf("%dd", int(secs/86400))
	}
}

// ── Vector glyphs (4x supersampled pure Go) ───────────────────────────────────

func renderDoorGlyph(size int, isOpen bool, col color.RGBA) *image.RGBA {
	const ss = 4
	w := size * ss
	hi := image.NewRGBA(image.Rect(0, 0, w, w))
	scale := float64(w) / 24.0

	fill := func(x, y float64) bool {
		gx := x / scale
		gy := y / scale
		if !isOpen {
			// Closed door: outer door panel [6..18, 3..21], doorknob at (15.5, 12) r=1.1
			if gx >= 5.5 && gx <= 18.5 && gy >= 3 && gy <= 21 {
				// Threshold at bottom
				if gy >= 19.2 {
					return true
				}
				// Cutout for handle
				if math.Hypot(gx-15.2, gy-12.0) <= 1.25 {
					return false
				}
				// Subtle inner panel groove
				if (gx >= 7.5 && gx <= 16.5 && gy >= 5 && gy <= 10.5) &&
					!(gx >= 8.5 && gx <= 15.5 && gy >= 6 && gy <= 9.5) {
					return false
				}
				return true
			}
			return false
		}
		// Open door: doorway frame on right [13..19, 4..20] (hollow), ajar door leaf on left
		// Bottom sill [3..21, 19.2..21]
		if gx >= 3 && gx <= 21 && gy >= 19.2 && gy <= 21 {
			return true
		}
		// Right frame upright [17.2..19, 3..20] and top lintel [12..19, 3..4.8]
		if gx >= 17.2 && gx <= 19 && gy >= 3 && gy <= 20 {
			return true
		}
		if gx >= 12 && gx <= 19 && gy >= 3 && gy <= 4.8 {
			return true
		}
		// Tilted ajar door panel from x=5..14, top slopes from y=5 (at x=5) to y=3 (at x=14),
		// bottom at y=19.2..20.5
		if gx >= 5 && gx <= 14 {
			t := (gx - 5.0) / 9.0
			topY := 5.0 - 2.0*t
			botY := 19.5
			if gy >= topY && gy <= botY {
				// Doorknob cutout near left/outer edge
				if math.Hypot(gx-7.2, gy-12.5) <= 1.15 {
					return false
				}
				return true
			}
		}
		return false
	}

	for py := 0; py < w; py++ {
		for px := 0; px < w; px++ {
			if fill(float64(px)+0.5, float64(py)+0.5) {
				hi.SetRGBA(px, py, col)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	downsampleOverlay(out, hi, ss)
	return out
}

func renderSproutGlyph(size int, col color.RGBA) *image.RGBA {
	const ss = 4
	w := size * ss
	hi := image.NewRGBA(image.Rect(0, 0, w, w))
	scale := float64(w) / 24.0

	fill := func(x, y float64) bool {
		gx := x / scale
		gy := y / scale
		// Soil/base mound
		if gx >= 7 && gx <= 17 && gy >= 19.5 && gy <= 21.5 {
			return true
		}
		// Main stem
		if gx >= 11.1 && gx <= 12.9 && gy >= 9.0 && gy <= 20.0 {
			return true
		}
		// Left leaf (tilted ellipse centered at (7.8, 10.5))
		{
			dx := gx - 7.8
			dy := gy - 10.5
			ca, sa := math.Cos(-0.55), math.Sin(-0.55)
			rx := dx*ca - dy*sa
			ry := dx*sa + dy*ca
			if (rx*rx)/(4.2*4.2)+(ry*ry)/(2.3*2.3) <= 1.0 {
				return true
			}
		}
		// Right leaf (higher, tilted opposite, centered at (16.2, 8.0))
		{
			dx := gx - 16.2
			dy := gy - 8.0
			ca, sa := math.Cos(0.55), math.Sin(0.55)
			rx := dx*ca - dy*sa
			ry := dx*sa + dy*ca
			if (rx*rx)/(4.4*4.4)+(ry*ry)/(2.4*2.4) <= 1.0 {
				return true
			}
		}
		return false
	}

	for py := 0; py < w; py++ {
		for px := 0; px < w; px++ {
			if fill(float64(px)+0.5, float64(py)+0.5) {
				hi.SetRGBA(px, py, col)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	downsampleOverlay(out, hi, ss)
	return out
}

func renderBellGlyph(size int, col color.RGBA) *image.RGBA {
	const ss = 4
	w := size * ss
	hi := image.NewRGBA(image.Rect(0, 0, w, w))
	scale := float64(w) / 24.0

	fill := func(x, y float64) bool {
		gx := x / scale
		gy := y / scale
		// Top knob
		if math.Hypot(gx-12.0, gy-3.5) <= 1.5 {
			return true
		}
		// Bell dome: y from 4.5 to 15.5, half-width curves from 2 to 6.5
		if gy >= 4.5 && gy <= 16.0 {
			t := (gy - 4.5) / 11.5
			halfW := 2.0 + 4.8*math.Sin(t*math.Pi*0.5)
			if t > 0.75 {
				halfW += (t - 0.75) * 6.0 // flared rim
			}
			if math.Abs(gx-12.0) <= halfW {
				return true
			}
		}
		// Bottom rim bar
		if gx >= 4.2 && gx <= 19.8 && gy >= 15.8 && gy <= 17.6 {
			return true
		}
		// Clapper
		if math.Hypot(gx-12.0, gy-19.3) <= 1.9 {
			return true
		}
		return false
	}

	for py := 0; py < w; py++ {
		for px := 0; px < w; px++ {
			if fill(float64(px)+0.5, float64(py)+0.5) {
				hi.SetRGBA(px, py, col)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	downsampleOverlay(out, hi, ss)
	return out
}

func renderMowerGlyph(size int, col color.RGBA) *image.RGBA {
	const ss = 4
	w := size * ss
	hi := image.NewRGBA(image.Rect(0, 0, w, w))
	scale := float64(w) / 24.0

	fill := func(x, y float64) bool {
		gx := x / scale
		gy := y / scale

		// Rear wheel cutout (left) at (6, 14) r=3 outer, r=1.5 inner
		dRear := math.Hypot(gx-6.0, gy-14.0)
		if dRear <= 3.0 && dRear >= 1.5 {
			return true
		}
		// Front wheel (right) at (18, 16.5) r=2.0
		if math.Hypot(gx-18.0, gy-16.5) <= 2.2 {
			return true
		}
		// Mower body: rounded/sloped chassis from x=2..21, y=7..16
		// Top slopes down from x=13 to x=21
		if gx >= 2.0 && gx <= 21.5 && gy >= 7.0 && gy <= 16.5 {
			topY := 7.0
			if gx > 13.0 {
				topY = 7.0 + (gx-13.0)*0.85
			}
			if gy >= topY {
				// Cut out wheel wells so wheels read distinctly
				if dRear <= 3.6 && dRear > 3.0 {
					return false
				}
				if math.Hypot(gx-18.0, gy-16.5) <= 2.8 && math.Hypot(gx-18.0, gy-16.5) > 2.2 {
					return false
				}
				return true
			}
		}
		return false
	}

	for py := 0; py < w; py++ {
		for px := 0; px < w; px++ {
			if fill(float64(px)+0.5, float64(py)+0.5) {
				hi.SetRGBA(px, py, col)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	downsampleOverlay(out, hi, ss)
	return out
}

// ── Subcommands ───────────────────────────────────────────────────────────────

func (c *haClient) cmdIconLight(entity, iconBase string) int {
	state := c.getState(entity)
	variant := "off"
	if state == "on" {
		variant = "on"
	}
	src := assetPath(filepath.Join("ha", "assets", fmt.Sprintf("%s-%s.png", iconBase, variant)))
	dst := filepath.Join(c.cache, iconBase+".png")

	if visualSignatureMatches(dst, variant) {
		fmt.Println(dst)
		return 0
	}
	if err := atomicCopyFile(src, dst); err != nil {
		fmt.Println(src)
		return 0
	}
	writeVisualSignature(dst, variant)
	fmt.Println(dst)
	return 0
}

func (c *haClient) cmdIconDoor(entity, cacheName, label string) int {
	out := filepath.Join(c.cache, "door-"+cacheName+".png")
	state, timeStr := c.getEntityStateAndTime(entity)
	isOpen := state == "on"

	timeLabel := "OPEN"
	if !isOpen {
		timeLabel = formatRelativeTime(timeStr, time.Now().UTC())
	}

	sig := fmt.Sprintf("door|%t|%s|%s", isOpen, timeLabel, label)
	if visualSignatureMatches(out, sig) {
		fmt.Println(out)
		return 0
	}

	img := renderDoorCard(isOpen, timeLabel, label)
	if err := atomicWritePNG(out, img); err != nil {
		printfErr("ha icon-door: %v\n", err)
		return 1
	}
	writeVisualSignature(out, sig)
	fmt.Println(out)
	return 0
}

func renderDoorCard(isOpen bool, timeLabel, label string) *image.RGBA {
	const size = 72
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRectSrc(img, 0, 0, size, size, color.RGBA{18, 18, 28, 255})

	iconCol := color.RGBA{85, 170, 255, 255}
	timeCol := color.RGBA{200, 200, 200, 255}
	if isOpen {
		iconCol = color.RGBA{255, 85, 85, 255}
		timeCol = color.RGBA{255, 85, 85, 255}
	}

	glyph := renderDoorGlyph(22, isOpen, iconCol)
	draw.Draw(img, image.Rect((size-22)/2, 4, (size-22)/2+22, 26), glyph, image.Point{}, draw.Over)

	bold, _ := loadDejaVuBold()
	reg, _ := loadDejaVuReg()
	fBig := newFace(bold, 24)
	fSm := newFace(reg, 10)
	if fBig != nil {
		defer fBig.Close()
		drawCenteredText(img, fBig, timeLabel, size/2, 28, timeCol)
	}
	if fSm != nil {
		defer fSm.Close()
		drawCenteredText(img, fSm, label, size/2, size-13, color.RGBA{160, 160, 180, 255})
	}
	return img
}

func (c *haClient) cmdIconMoisture(entity, cacheName, label string) int {
	out := filepath.Join(c.cache, "plant-"+cacheName+".png")
	st := c.getState(entity)
	moisture, err := strconv.ParseFloat(strings.TrimSpace(st), 64)
	if err != nil {
		moisture = 0.0
	}

	tier := "wet"
	if moisture < 25 {
		tier = "dry"
	} else if moisture < 65 {
		tier = "ok"
	}
	clamped := moisture
	if clamped < 0 {
		clamped = 0
	} else if clamped > 100 {
		clamped = 100
	}
	const barH = 20
	fillH := int(float64(barH) * clamped / 100.0)
	pct := fmt.Sprintf("%d%%", int(moisture))

	sig := fmt.Sprintf("moist|%s|%s|%d|%s", pct, tier, fillH, label)
	if visualSignatureMatches(out, sig) {
		fmt.Println(out)
		return 0
	}

	img := renderMoistureCard(moisture, label)
	if err := atomicWritePNG(out, img); err != nil {
		printfErr("ha icon-moisture: %v\n", err)
		return 1
	}
	writeVisualSignature(out, sig)
	fmt.Println(out)
	return 0
}

func renderMoistureCard(moisture float64, label string) *image.RGBA {
	const size = 72
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRectSrc(img, 0, 0, size, size, color.RGBA{18, 18, 28, 255})

	var iconCol, timeCol color.RGBA
	if moisture < 25 {
		iconCol = color.RGBA{221, 102, 34, 255}
		timeCol = color.RGBA{220, 100, 30, 255}
	} else if moisture < 65 {
		iconCol = color.RGBA{68, 204, 85, 255}
		timeCol = color.RGBA{70, 200, 80, 255}
	} else {
		iconCol = color.RGBA{68, 153, 238, 255}
		timeCol = color.RGBA{60, 140, 220, 255}
	}

	glyph := renderSproutGlyph(20, iconCol)
	draw.Draw(img, image.Rect(4, 4, 24, 24), glyph, image.Point{}, draw.Over)

	const barX, barY, barW, barH = 28, 4, 8, 20
	fillRect(img, barX, barY, barX+barW, barY+barH, color.RGBA{45, 45, 60, 255})

	clamped := moisture
	if clamped < 0 {
		clamped = 0
	} else if clamped > 100 {
		clamped = 100
	}
	fillH := int(float64(barH) * clamped / 100.0)
	if fillH > 0 {
		barFill := color.RGBA{
			R: uint8(float64(timeCol.R) * 0.85),
			G: uint8(float64(timeCol.G) * 0.85),
			B: uint8(float64(timeCol.B) * 0.85),
			A: 255,
		}
		fillRect(img, barX+1, barY+barH-fillH, barX+barW-1, barY+barH-1, barFill)
	}

	pct := fmt.Sprintf("%d%%", int(moisture))
	bold, _ := loadDejaVuBold()
	reg, _ := loadDejaVuReg()
	fBig := newFace(bold, 22)
	fSm := newFace(reg, 10)
	if fBig != nil {
		defer fBig.Close()
		drawCenteredText(img, fBig, pct, size/2, 27, timeCol)
	}
	if fSm != nil {
		defer fSm.Close()
		drawCenteredText(img, fSm, label, size/2, size-13, color.RGBA{160, 160, 180, 255})
	}
	return img
}

func (c *haClient) cmdIconBell(entity, cacheName, label string) int {
	out := filepath.Join(c.cache, "bell-"+cacheName+".png")
	state, timeStr := c.getEntityStateAndTime(entity)
	isRinging := state == "on"

	timeLabel := "NOW!"
	if !isRinging {
		timeLabel = formatRelativeTime(timeStr, time.Now().UTC())
	}

	sig := fmt.Sprintf("bell|%t|%s|%s", isRinging, timeLabel, label)
	if visualSignatureMatches(out, sig) {
		fmt.Println(out)
		return 0
	}

	img := renderBellCard(isRinging, timeLabel, label)
	if err := atomicWritePNG(out, img); err != nil {
		printfErr("ha icon-bell: %v\n", err)
		return 1
	}
	writeVisualSignature(out, sig)
	fmt.Println(out)
	return 0
}

func renderBellCard(isRinging bool, timeLabel, label string) *image.RGBA {
	const size = 72
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRectSrc(img, 0, 0, size, size, color.RGBA{18, 18, 28, 255})

	iconCol := color.RGBA{255, 170, 51, 255}
	timeCol := color.RGBA{210, 160, 80, 255}
	if isRinging {
		iconCol = color.RGBA{255, 221, 0, 255}
		timeCol = color.RGBA{255, 220, 0, 255}
	}

	glyph := renderBellGlyph(22, iconCol)
	draw.Draw(img, image.Rect((size-22)/2, 4, (size-22)/2+22, 26), glyph, image.Point{}, draw.Over)

	bold, _ := loadDejaVuBold()
	reg, _ := loadDejaVuReg()
	fBig := newFace(bold, 24)
	fSm := newFace(reg, 10)
	if fBig != nil {
		defer fBig.Close()
		drawCenteredText(img, fBig, timeLabel, size/2, 28, timeCol)
	}
	if fSm != nil {
		defer fSm.Close()
		drawCenteredText(img, fSm, label, size/2, size-13, color.RGBA{160, 160, 180, 255})
	}
	return img
}

func (c *haClient) cmdIconCamera(entity, cacheName, label string) int {
	out := filepath.Join(c.cache, "camera-"+cacheName+".png")
	const size = 72

	var img *image.RGBA
	if raw, err := c.haGet("/api/camera_proxy/" + entity); err == nil {
		if decoded, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
			b := decoded.Bounds()
			w, h := b.Dx(), b.Dy()
			crop := minInt(w, h)
			left := b.Min.X + (w-crop)/2
			top := b.Min.Y + (h-crop)/2
			cropped := image.NewRGBA(image.Rect(0, 0, crop, crop))
			draw.Draw(cropped, cropped.Bounds(), decoded, image.Pt(left, top), draw.Src)
			img = resizeCatmullRom(cropped, size, size)
		}
	}
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, size, size))
		fillRectSrc(img, 0, 0, size, size, color.RGBA{18, 18, 28, 255})
	}

	// Translucent label bar at bottom
	fillRect(img, 0, size-18, size, size, color.RGBA{0, 0, 0, 160})
	reg, _ := loadDejaVuReg()
	if fSm := newFace(reg, 10); fSm != nil {
		defer fSm.Close()
		drawCenteredText(img, fSm, label, size/2, size-14, color.RGBA{220, 220, 220, 255})
	}

	if err := atomicWritePNG(out, img); err != nil {
		printfErr("ha icon-camera: %v\n", err)
		return 1
	}
	fmt.Println(out)
	return 0
}

func (c *haClient) cmdIconMowerBadge(entity string) int {
	baseSrc := assetPath(filepath.Join("ha", "assets", "ha.png"))
	out := filepath.Join(c.cache, "ha-mower.png")

	state := strings.ToLower(strings.TrimSpace(c.getState(entity)))
	healthy := false
	switch state {
	case "no_error", "unavailable", "unknown", "", "?", "rain_delay":
		healthy = true
	}

	sig := "mower|err"
	if healthy {
		sig = "mower|ok"
	}
	if visualSignatureMatches(out, sig) {
		fmt.Println(out)
		return 0
	}

	f, err := os.Open(baseSrc)
	if err != nil {
		fmt.Println(baseSrc)
		return 0
	}
	baseImg, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		fmt.Println(baseSrc)
		return 0
	}

	img := renderMowerBadge(baseImg, healthy)
	if err := atomicWritePNG(out, img); err != nil {
		fmt.Println(baseSrc)
		return 0
	}
	writeVisualSignature(out, sig)
	fmt.Println(out)
	return 0
}

func renderMowerBadge(base image.Image, healthy bool) *image.RGBA {
	b := base.Bounds()
	w, h := b.Dx(), b.Dy()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), base, b.Min, draw.Src)

	if healthy {
		return img
	}

	r := maxInt(12, w/4)
	cx := float64(w - r - 1)
	cy := float64(h - r - 1)
	drawCircleAA(img, cx, cy, float64(r+2), color.RGBA{255, 255, 255, 255})
	drawCircleAA(img, cx, cy, float64(r), color.RGBA{220, 38, 38, 255})

	gSize := int(float64(r) * 1.7)
	glyph := renderMowerGlyph(gSize, color.RGBA{255, 255, 255, 255})
	gx := int(cx) - gSize/2
	gy := int(cy) - gSize/2
	draw.Draw(img, image.Rect(gx, gy, gx+gSize, gy+gSize), glyph, image.Point{}, draw.Over)
	return img
}

func (c *haClient) cmdToggle(domain, entity string) int {
	_ = c.haPost(fmt.Sprintf("/api/services/%s/toggle", domain), map[string]string{"entity_id": entity})
	c.invalidateStatesCache()
	return 0
}

func (c *haClient) pushDeckAlert(label string) {
	payload := map[string]any{
		"label":      label,
		"color":      "#ffffff",
		"background": "#cc2200",
		"fontsize":   12,
		"duration":   "20s",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, c.deckAPI+"/key/7", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (c *haClient) cmdPollDoors() int {
	type doorSpec struct {
		id     string
		entity string
		label  string
	}
	doors := []doorSpec{
		{"3", "binary_sensor.haustur_contact", "🚪 HAUSTÜR"},
		{"4", "binary_sensor.terrassentur_contact", "🚪 TERRASSE"},
		{"5", "binary_sensor.kuchentur_contact", "🚪 KÜCHENTÜR"},
		{"6", "binary_sensor.fahrrad_box_contact", "🚪 FAHRRAD"},
		{"10", "binary_sensor.schlafzimmer_fenster_contact", "🪟 SCHLAFZ.FENSTER"},
		{"klingel", "binary_sensor.0x00158d008c7c34b5_vibration", "🔔 KLINGEL"},
	}

	prev := make(map[string]string, len(doors))
	for {
		for _, d := range doors {
			state := c.getState(d.entity)
			if prev[d.id] == "off" && state == "on" {
				c.pushDeckAlert(d.label)
			}
			prev[d.id] = state
		}
		time.Sleep(5 * time.Second)
	}
}

// RunHA is the entrypoint for `./deckctl ha <subcmd> ...`.
func RunHA(args []string) int {
	if len(args) < 1 {
		printfErr("usage: ha <icon-light|icon-door|icon-moisture|icon-bell|icon-camera|icon-mower-badge|toggle-switch|toggle-light|poll-doors> ...\n")
		return 1
	}

	c := newHAClient()
	_ = os.MkdirAll(c.cache, 0o755)

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "icon-light":
		if len(rest) < 2 {
			return 1
		}
		return c.cmdIconLight(rest[0], rest[1])
	case "icon-door":
		if len(rest) < 3 {
			return 1
		}
		return c.cmdIconDoor(rest[0], rest[1], strings.Join(rest[2:], " "))
	case "icon-moisture":
		if len(rest) < 3 {
			return 1
		}
		return c.cmdIconMoisture(rest[0], rest[1], strings.Join(rest[2:], " "))
	case "icon-bell":
		if len(rest) < 3 {
			return 1
		}
		return c.cmdIconBell(rest[0], rest[1], strings.Join(rest[2:], " "))
	case "icon-camera":
		if len(rest) < 3 {
			return 1
		}
		return c.cmdIconCamera(rest[0], rest[1], strings.Join(rest[2:], " "))
	case "icon-mower-badge":
		if len(rest) < 1 {
			return 1
		}
		return c.cmdIconMowerBadge(rest[0])
	case "toggle-switch":
		if len(rest) < 1 {
			return 1
		}
		return c.cmdToggle("switch", rest[0])
	case "toggle-light":
		if len(rest) < 1 {
			return 1
		}
		return c.cmdToggle("light", rest[0])
	case "poll-doors":
		return c.cmdPollDoors()
	default:
		printfErr("unknown ha subcommand: %s\n", cmd)
		return 1
	}
}
