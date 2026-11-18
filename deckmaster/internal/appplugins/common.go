package appplugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const keySize = 72

var (
	// apiBase is the deckmaster HTTP API base URL (overridable in tests).
	apiBase    = "http://localhost:9990"
	httpClient = &http.Client{Timeout: 3 * time.Second}

	fontMu    sync.Mutex
	fontCache = map[string]*truetype.Font{}
)

const (
	fontDejaVuSansBold     = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
	fontDejaVuSans         = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
	fontDejaVuSansMonoBold = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
	fontDejaVuSansMono     = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
)

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// resolveAsset resolves a path relative to the deckmaster working directory.
// If CWD is already deckmaster/, rel is returned if it exists; otherwise it
// walks up parent directories (helpful when running go test inside internal/appplugins).
func resolveAsset(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	dir, err := os.Getwd()
	if err == nil {
		for i := 0; i < 4; i++ {
			candidate := filepath.Join(dir, rel)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return rel
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

func savePNGAtomic(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes(), 0o644)
}

func pushIcon(key int, iconPath string) {
	payload, err := json.Marshal(map[string]string{"icon": iconPath})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/key/%d", apiBase, key), bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API error key %d: %v\n", key, err)
		return
	}
	_ = resp.Body.Close()
}

func pushText(key int, label, fg, bg string, fontsize int) {
	payload, err := json.Marshal(map[string]any{
		"label":      label,
		"color":      fg,
		"background": bg,
		"fontsize":   fontsize,
	})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/key/%d", apiBase, key), bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API error key %d: %v\n", key, err)
		return
	}
	_ = resp.Body.Close()
}

func loadTTF(path string) *truetype.Font {
	fontMu.Lock()
	defer fontMu.Unlock()
	if f, ok := fontCache[path]; ok {
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	f, err := truetype.Parse(data)
	if err != nil {
		return nil
	}
	fontCache[path] = f
	return f
}

func getFontFace(ttfPath string, size float64) font.Face {
	if f := loadTTF(ttfPath); f != nil {
		return truetype.NewFace(f, &truetype.Options{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
	}
	return basicfont.Face7x13
}

func measureText(face font.Face, text string) (int, int) {
	b, _ := font.BoundString(face, text)
	w := (b.Max.X - b.Min.X).Ceil()
	h := (b.Max.Y - b.Min.Y).Ceil()
	if w <= 0 && text != "" {
		w = font.MeasureString(face, text).Ceil()
	}
	if h <= 0 {
		h = face.Metrics().Ascent.Ceil()
	}
	return w, h
}

func drawTextTopLeft(img *image.RGBA, x, y int, text string, col color.Color, face font.Face) {
	if text == "" {
		return
	}
	b, _ := font.BoundString(face, text)
	// Align top of ink bounding box to y, left of ink bounding box to x.
	dotX := x - b.Min.X.Floor()
	dotY := y - b.Min.Y.Floor()
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(dotX, dotY),
	}
	d.DrawString(text)
}

func newRGBA(bg color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	return img
}

func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if x < 0 || y < 0 || x >= keySize || y >= keySize || c.A == 0 {
		return
	}
	if c.A == 255 {
		img.SetRGBA(x, y, c)
		return
	}
	dst := img.RGBAAt(x, y)
	sa := uint32(c.A)
	inv := 255 - sa
	r := uint8((uint32(c.R)*sa + uint32(dst.R)*inv) / 255)
	g := uint8((uint32(c.G)*sa + uint32(dst.G)*inv) / 255)
	b := uint8((uint32(c.B)*sa + uint32(dst.B)*inv) / 255)
	a := uint8(sa + (uint32(dst.A)*inv)/255)
	img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: a})
}

func drawGradientV(img *image.RGBA, x0, y0, x1, y1 int, top, bot color.RGBA) {
	h := y1 - y0
	if h < 1 {
		h = 1
	}
	for y := y0; y < y1; y++ {
		t := float64(y-y0) / float64(h)
		c := color.RGBA{
			R: uint8(float64(top.R) + (float64(bot.R)-float64(top.R))*t),
			G: uint8(float64(top.G) + (float64(bot.G)-float64(top.G))*t),
			B: uint8(float64(top.B) + (float64(bot.B)-float64(top.B))*t),
			A: uint8(float64(top.A) + (float64(bot.A)-float64(top.A))*t),
		}
		for x := x0; x < x1; x++ {
			blendPixel(img, x, y, c)
		}
	}
}

func pointInRoundedRect(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	if r <= 0 {
		return true
	}
	// Check 4 corners
	cx0, cy0 := x0+r, y0+r
	cx1, cy1 := x1-r, y1-r
	if px < cx0 && py < cy0 {
		dx, dy := px-cx0, py-cy0
		return dx*dx+dy*dy <= r*r
	}
	if px > cx1 && py < cy0 {
		dx, dy := px-cx1, py-cy0
		return dx*dx+dy*dy <= r*r
	}
	if px < cx0 && py > cy1 {
		dx, dy := px-cx0, py-cy1
		return dx*dx+dy*dy <= r*r
	}
	if px > cx1 && py > cy1 {
		dx, dy := px-cx1, py-cy1
		return dx*dx+dy*dy <= r*r
	}
	return true
}

func drawRoundedRect(img *image.RGBA, x0, y0, x1, y1, radius int, fill *color.RGBA, outline *color.RGBA, width int) {
	fx0, fy0, fx1, fy1 := float64(x0), float64(y0), float64(x1), float64(y1)
	fr := float64(radius)
	fw := float64(width)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			px, py := float64(x), float64(y)
			if !pointInRoundedRect(px, py, fx0, fy0, fx1, fy1, fr) {
				continue
			}
			isBorder := false
			if outline != nil && width > 0 {
				innerR := math.Max(0, fr-fw)
				if !pointInRoundedRect(px, py, fx0+fw, fy0+fw, fx1-fw, fy1-fw, innerR) {
					isBorder = true
				}
			}
			if isBorder {
				blendPixel(img, x, y, *outline)
			} else if fill != nil {
				blendPixel(img, x, y, *fill)
			}
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				blendPixel(img, x, y, col)
			}
		}
	}
}

func strokeCircle(img *image.RGBA, cx, cy, r, width int, col color.RGBA) {
	outer2 := float64(r * r)
	inner := float64(r - width)
	if inner < 0 {
		inner = 0
	}
	inner2 := inner * inner
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := float64(x-cx), float64(y-cy)
			d2 := dx*dx + dy*dy
			if d2 <= outer2 && d2 >= inner2 {
				blendPixel(img, x, y, col)
			}
		}
	}
}

func loadPNG(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst, nil
}

func parseHexColor(h string) color.RGBA {
	h = strings.TrimPrefix(strings.TrimSpace(h), "#")
	if len(h) != 6 {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	r, err1 := strconv.ParseUint(h[0:2], 16, 8)
	g, err2 := strconv.ParseUint(h[2:4], 16, 8)
	b, err3 := strconv.ParseUint(h[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}
