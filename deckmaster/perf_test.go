package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muesli/streamdeck"
)

func writeTempPNG(t *testing.T, dir, name string, size int, c color.RGBA) string {
	t.Helper()
	path := filepath.Join(dir, name)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode temp png: %v", err)
	}
	return path
}

func TestRunCommandFastPaths(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "empty", cmd: "", want: ""},
		{name: "whitespace", cmd: "   \t  ", want: ""},
		{name: "echo plain", cmd: "echo /tmp/foo.png", want: "/tmp/foo.png"},
		{name: "echo double quoted", cmd: `echo "/tmp/foo.png"`, want: "/tmp/foo.png"},
		{name: "echo single quoted", cmd: `echo '/tmp/foo.png'`, want: "/tmp/foo.png"},
		{name: "direct exec no shell", cmd: "printf direct_ok", want: "direct_ok"},
		{name: "shell fallback pipe", cmd: "echo abc | tr a-c x-z", want: "xyz"},
		{name: "shell fallback quotes", cmd: "printf '%s' 'hello world'", want: "hello world"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runCommand(tc.cmd)
			if err != nil {
				t.Fatalf("runCommand(%q) unexpected error: %v", tc.cmd, err)
			}
			if got != tc.want {
				t.Fatalf("runCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestBuildExecCommand(t *testing.T) {
	if c := buildExecCommand("   "); c != nil {
		t.Fatalf("expected nil for empty command, got %v", c)
	}

	direct := buildExecCommand("python3 scripts/foo.py --flag 1")
	if direct == nil {
		t.Fatal("expected non-nil direct command")
	}
	if len(direct.Args) != 4 || direct.Args[0] != "python3" || direct.Args[3] != "1" {
		t.Fatalf("unexpected direct args: %#v", direct.Args)
	}

	shell := buildExecCommand("echo hi | cat")
	if shell == nil {
		t.Fatal("expected non-nil shell command")
	}
	if len(shell.Args) != 3 || shell.Args[0] != "sh" || shell.Args[1] != "-c" {
		t.Fatalf("expected sh -c for shell metacharacters, got %#v", shell.Args)
	}
}

func TestCommandWidgetEmptyCommandFiltering(t *testing.T) {
	dev := &streamdeck.Device{Pixels: 72, Keys: 15, DPI: 72}
	bw := NewBaseWidget(dev, ".", 0, nil, nil, nil)

	wEmpty := NewCommandWidget(bw, WidgetConfig{
		ID: "command",
		Config: map[string]interface{}{
			"command": "",
		},
	})
	if wEmpty.commands != nil {
		t.Fatalf("expected nil commands for empty command, got %#v", wEmpty.commands)
	}
	if wEmpty.fonts != nil || wEmpty.colors != nil || wEmpty.frames != nil {
		t.Fatalf("expected nil fonts/colors/frames when all commands are empty")
	}

	wMixed := NewCommandWidget(bw, WidgetConfig{
		ID: "command",
		Config: map[string]interface{}{
			"command": " ; echo hello ;   ",
			"font":    "thin;bold;regular",
			"color":   "#111111;#222222;#333333",
		},
	})
	if len(wMixed.commands) != 1 || wMixed.commands[0] != " echo hello " {
		t.Fatalf("unexpected filtered commands: %#v", wMixed.commands)
	}
	if len(wMixed.fonts) != 1 || wMixed.fonts[0] != "bold" {
		t.Fatalf("unexpected aligned fonts: %#v", wMixed.fonts)
	}
	if len(wMixed.colors) != 1 {
		t.Fatalf("unexpected aligned colors len: %d", len(wMixed.colors))
	}
	r, g, b, _ := wMixed.colors[0].RGBA()
	if uint8(r>>8) != 0x22 || uint8(g>>8) != 0x22 || uint8(b>>8) != 0x22 {
		t.Fatalf("expected aligned color #222222, got (%02x,%02x,%02x)", r>>8, g>>8, b>>8)
	}
}

func TestCommandWidgetStatCache(t *testing.T) {
	dir := t.TempDir()
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	pngPath := writeTempPNG(t, dir, "icon.png", 72, red)

	dev := &streamdeck.Device{Pixels: 72, Keys: 15, DPI: 72}
	// Pre-populate keyImages with matching pixels so render() hits skipSetImage
	// without needing a physical USB device handle, and isolate deck generation.
	currentDeckGen.Store(1)
	defer currentDeckGen.Store(0)

	bw := NewBaseWidget(dev, dir, 3, nil, nil, nil)
	bw.setDeckGen(1)

	preImg := image.NewRGBA(image.Rect(0, 0, 72, 72))
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ {
			preImg.SetRGBA(x, y, red)
		}
	}
	keyImagesMu.Lock()
	keyImages[3] = preImg
	keyImagesValid[3] = true
	keyImagesMu.Unlock()
	defer InvalidateKeyImagesCache()

	w := NewCommandWidget(bw, WidgetConfig{
		ID: "command",
		Config: map[string]interface{}{
			"command": "",
			"icon":    pngPath,
		},
	})

	if err := w.Update(); err != nil {
		t.Fatalf("first Update failed: %v", err)
	}
	firstCached := w.cachedImg
	if firstCached == nil {
		t.Fatal("expected cachedImg to be populated after first Update")
	}

	// Second Update with unchanged file should reuse exact same *image.RGBA pointer.
	if err := w.Update(); err != nil {
		t.Fatalf("second Update failed: %v", err)
	}
	if w.cachedImg != firstCached {
		t.Fatal("expected cachedImg pointer to be reused on stat cache hit")
	}

	// Modify file with different size/modtime -> cache must miss and reload.
	// Ensure modtime changes even on coarse filesystems.
	time.Sleep(15 * time.Millisecond)
	writeTempPNG(t, dir, "icon.png", 72, blue)
	for y := 0; y < 72; y++ {
		for x := 0; x < 72; x++ {
			preImg.SetRGBA(x, y, blue)
		}
	}
	keyImagesMu.Lock()
	keyImages[3] = preImg
	keyImagesValid[3] = true
	keyImagesMu.Unlock()

	if err := w.Update(); err != nil {
		t.Fatalf("third Update failed: %v", err)
	}
	if w.cachedImg == firstCached {
		t.Fatal("expected cachedImg to be replaced after file modification")
	}
	if got := w.cachedImg.RGBAAt(10, 10); got != blue {
		t.Fatalf("expected blue pixel after reload, got %+v", got)
	}
}

func TestDrawImageNoResizeFastPath(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 72, 72))
	want := color.RGBA{12, 34, 56, 255}
	src.SetRGBA(10, 20, want)

	dst := image.NewRGBA(image.Rect(0, 0, 72, 72))
	if err := drawImage(dst, src, 72, image.Pt(0, 0)); err != nil {
		t.Fatalf("drawImage failed: %v", err)
	}
	if got := dst.RGBAAt(10, 20); got != want {
		t.Fatalf("dst.RGBAAt(10,20) = %+v, want %+v", got, want)
	}
}

func TestDeckGenerationPreventsStaleRender(t *testing.T) {
	currentDeckGen.Store(10)
	defer currentDeckGen.Store(0)

	dev := &streamdeck.Device{Pixels: 72, Keys: 15, DPI: 72}
	bw := NewBaseWidget(dev, ".", 4, nil, nil, nil)
	bw.setDeckGen(9) // stale generation

	InvalidateKeyImagesCache()
	if err := bw.render(dev, image.NewRGBA(image.Rect(0, 0, 72, 72))); err != nil {
		t.Fatalf("stale render returned error: %v", err)
	}

	keyImagesMu.RLock()
	valid := keyImagesValid[4]
	keyImagesMu.RUnlock()
	if valid {
		t.Fatal("stale generation render should not populate keyImages")
	}
}

func TestInFlightUpdateGuard(t *testing.T) {
	dev := &streamdeck.Device{Pixels: 72, Keys: 15, DPI: 72}
	bw := NewBaseWidget(dev, ".", 0, nil, nil, nil)

	if !bw.RequiresUpdate() {
		t.Fatal("expected fresh widget to require update")
	}
	if !bw.tryStartUpdate() {
		t.Fatal("expected first tryStartUpdate to succeed")
	}
	if bw.RequiresUpdate() {
		t.Fatal("expected RequiresUpdate to be false while updating")
	}
	if bw.tryStartUpdate() {
		t.Fatal("expected second tryStartUpdate to fail while updating")
	}
	bw.finishUpdate()
	if !bw.RequiresUpdate() {
		t.Fatal("expected RequiresUpdate to be true after finishUpdate before render")
	}
}

func TestCommandWidgetTildeIconExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dev := &streamdeck.Device{Pixels: 72, Keys: 15, DPI: 72}
	bw := NewBaseWidget(dev, ".", 0, nil, nil, nil)
	w := NewCommandWidget(bw, WidgetConfig{
		ID: "command",
		Config: map[string]interface{}{
			"icon_command": "echo ~/.local/share/deckblaster/calendar-day/key-0.png",
			"command":      "",
		},
	})

	got := w.resolveIconPath()
	want := filepath.Join(home, ".local", "share", "deckblaster", "calendar-day", "key-0.png")
	if got != want {
		t.Fatalf("resolveIconPath() = %q, want %q", got, want)
	}
}
