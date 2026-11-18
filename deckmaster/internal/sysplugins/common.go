package sysplugins

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

const (
	dejaVuBoldPath = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
	dejaVuRegPath  = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
)

var (
	baseDirOnce sync.Once
	cachedBase  string

	boldFontOnce sync.Once
	boldFont     *truetype.Font
	boldFontErr  error

	regFontOnce sync.Once
	regFont     *truetype.Font
	regFontErr  error
)

// resolveBaseDir returns the deckmaster root directory containing "decks/".
func resolveBaseDir() string {
	baseDirOnce.Do(func() {
		if cwd, err := os.Getwd(); err == nil {
			dir := cwd
			for i := 0; i < 4; i++ {
				if st, err := os.Stat(filepath.Join(dir, "decks")); err == nil && st.IsDir() {
					cachedBase = dir
					return
				}
				dir = filepath.Dir(dir)
			}
		}
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			for i := 0; i < 3; i++ {
				if st, err := os.Stat(filepath.Join(dir, "decks")); err == nil && st.IsDir() {
					cachedBase = dir
					return
				}
				dir = filepath.Dir(dir)
			}
		}
		if cwd, err := os.Getwd(); err == nil {
			cachedBase = cwd
		} else {
			cachedBase = "."
		}
	})
	return cachedBase
}

// assetPath resolves a path relative to <deckmaster>/decks/.
func assetPath(rel string) string {
	return filepath.Join(resolveBaseDir(), "decks", rel)
}

// atomicWriteFile writes data to path atomically via a temp file in the same directory.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
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

// atomicWritePNG encodes img as PNG and writes it atomically to path.
func atomicWritePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return atomicWriteFile(path, buf.Bytes(), 0o644)
}

// atomicCopyFile copies src to dst atomically.
func atomicCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
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

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func loadDejaVuBold() (*truetype.Font, error) {
	boldFontOnce.Do(func() {
		b, err := os.ReadFile(dejaVuBoldPath)
		if err != nil {
			boldFontErr = err
			return
		}
		boldFont, boldFontErr = truetype.Parse(b)
	})
	return boldFont, boldFontErr
}

func loadDejaVuReg() (*truetype.Font, error) {
	regFontOnce.Do(func() {
		b, err := os.ReadFile(dejaVuRegPath)
		if err != nil {
			regFontErr = err
			return
		}
		regFont, regFontErr = truetype.Parse(b)
	})
	return regFont, regFontErr
}

func newFace(ttf *truetype.Font, sizePt float64) font.Face {
	if ttf == nil {
		return nil
	}
	return truetype.NewFace(ttf, &truetype.Options{
		Size:    sizePt,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// drawCenteredText draws text centered horizontally at y (top of text box) or baseline.
// If baseline is false, y is treated as the top edge of the text (matching PIL's default anchor="la" y).
func drawCenteredText(img *image.RGBA, face font.Face, text string, centerX, y int, col color.Color) {
	if face == nil || text == "" {
		return
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
	}
	adv := d.MeasureString(text)
	w := adv.Round()
	ascent := face.Metrics().Ascent.Round()
	d.Dot = fixed.P(centerX-w/2, y+ascent)
	d.DrawString(text)
}

func measureTextWidth(face font.Face, text string) int {
	if face == nil || text == "" {
		return 0
	}
	d := &font.Drawer{Face: face}
	return d.MeasureString(text).Round()
}

// fillRect fills [x0, y0, x1, y1) with col using draw.Over.
func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.Color) {
	r := image.Rect(x0, y0, x1, y1).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(img, r, image.NewUniform(col), image.Point{}, draw.Over)
}

// fillRectSrc fills [x0, y0, x1, y1) with col using draw.Src.
func fillRectSrc(img *image.RGBA, x0, y0, x1, y1 int, col color.Color) {
	r := image.Rect(x0, y0, x1, y1).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(img, r, image.NewUniform(col), image.Point{}, draw.Src)
}

// drawCircleAA draws an anti-aliased filled circle on img.
func drawCircleAA(img *image.RGBA, cx, cy, r float64, col color.RGBA) {
	b := img.Bounds()
	minX := maxInt(b.Min.X, int(math.Floor(cx-r-1)))
	maxX := minInt(b.Max.X-1, int(math.Ceil(cx+r+1)))
	minY := maxInt(b.Min.Y, int(math.Floor(cy-r-1)))
	maxY := minInt(b.Max.Y-1, int(math.Ceil(cy+r+1)))

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			dist := math.Hypot(dx, dy)
			cov := clamp01(r + 0.5 - dist)
			if cov > 0 {
				blendPixel(img, x, y, col, cov)
			}
		}
	}
}

// drawRoundedRectOutline draws a 1px anti-aliased rounded rectangle outline.
func drawRoundedRectOutline(img *image.RGBA, x0, y0, x1, y1, radius float64, col color.RGBA) {
	// Use 4x supersampling on a small mask for crisp rounded rect outline
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	const ss = 4
	hi := image.NewRGBA(image.Rect(0, 0, w*ss, h*ss))
	fx0, fy0 := x0*ss, y0*ss
	fx1, fy1 := x1*ss, y1*ss
	fr := radius * ss
	stroke := float64(ss)

	for py := 0; py < h*ss; py++ {
		for px := 0; px < w*ss; px++ {
			x := float64(px) + 0.5
			y := float64(py) + 0.5
			dOut := sdfRoundedRect(x, y, fx0, fy0, fx1, fy1, fr)
			if dOut <= 0 && dOut >= -stroke {
				hi.SetRGBA(px, py, col)
			}
		}
	}
	downsampleOverlay(img, hi, ss)
}

func sdfRoundedRect(x, y, x0, y0, x1, y1, r float64) float64 {
	cx := (x0 + x1) * 0.5
	cy := (y0 + y1) * 0.5
	hx := (x1-x0)*0.5 - r
	hy := (y1-y0)*0.5 - r
	dx := math.Abs(x-cx) - hx
	dy := math.Abs(y-cy) - hy
	ox := math.Max(dx, 0)
	oy := math.Max(dy, 0)
	return math.Hypot(ox, oy) + math.Min(math.Max(dx, dy), 0) - r
}

func downsampleOverlay(dst *image.RGBA, hi *image.RGBA, ss int) {
	b := dst.Bounds()
	scale := float64(ss * ss)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var rSum, gSum, bSum, aSum float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					c := hi.RGBAAt(x*ss+sx, y*ss+sy)
					a := float64(c.A) / 255.0
					rSum += float64(c.R) * a
					gSum += float64(c.G) * a
					bSum += float64(c.B) * a
					aSum += float64(c.A)
				}
			}
			if aSum <= 0 {
				continue
			}
			avgA := aSum / scale
			cov := avgA / 255.0
			col := color.RGBA{
				R: uint8(math.Round(rSum / (aSum / 255.0))),
				G: uint8(math.Round(gSum / (aSum / 255.0))),
				B: uint8(math.Round(bSum / (aSum / 255.0))),
				A: 255,
			}
			blendPixel(dst, x, y, col, cov)
		}
	}
}

func blendPixel(img *image.RGBA, x, y int, col color.RGBA, cov float64) {
	if cov <= 0 {
		return
	}
	if cov > 1 {
		cov = 1
	}
	a := (float64(col.A) / 255.0) * cov
	dst := img.RGBAAt(x, y)
	inv := 1.0 - a
	outR := uint8(math.Round(float64(col.R)*a + float64(dst.R)*inv))
	outG := uint8(math.Round(float64(col.G)*a + float64(dst.G)*inv))
	outB := uint8(math.Round(float64(col.B)*a + float64(dst.B)*inv))
	outA := uint8(math.Round(255.0*a + float64(dst.A)*inv))
	img.SetRGBA(x, y, color.RGBA{R: outR, G: outG, B: outB, A: outA})
}

func resizeCatmullRom(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// loadDotEnvFallback reads KEY=VAL pairs from ~/.config/deckblaster.env if env vars are missing.
func getEnvOrConfig(key, defVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".config", "deckblaster.env"))
		if err == nil {
			prefix := key + "="
			for _, line := range bytes.Split(data, []byte("\n")) {
				s := string(bytes.TrimSpace(line))
				if len(s) > len(prefix) && s[:len(prefix)] == prefix {
					val := s[len(prefix):]
					if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
						val = val[1 : len(val)-1]
					}
					if val != "" {
						return val
					}
				}
			}
		}
	}
	return defVal
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func printfErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
