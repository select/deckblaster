package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/bendahl/uinput"
	"github.com/fsnotify/fsnotify"
	"github.com/godbus/dbus"
	"github.com/mitchellh/go-homedir"
	"github.com/muesli/streamdeck"
)

var (
	// Version contains the application version number. It's set via ldflags
	// when building.
	Version = ""

	// CommitSHA contains the SHA of the commit that this application was built
	// against. It's set via ldflags when building.
	CommitSHA = ""

	deck *Deck

	// keyLocks holds per-key override expiry times set by the HTTP API.
	// While a key is locked the render loop skips it, preventing command
	// widgets from overwriting a temporary alert background.
	keyLocks sync.Map // uint8 → time.Time

	dbusConn *dbus.Conn
	keyboard uinput.Keyboard
	shutdown = make(chan error)

	xorg          *Xorg
	recentWindows []Window

	deckFile   = flag.String("deck", "main.deck", "path to deck config file")
	device     = flag.String("device", "", "which device to use (serial number)")
	brightness = flag.Uint("brightness", 80, "brightness in percent")
	sleep      = flag.String("sleep", "", "sleep timeout")
	apiAddr    = flag.String("api", "", "HTTP API listen address (e.g. :9990 or /tmp/deckmaster.sock)")
	watch      = flag.Bool("watch", false, "watch deck file for changes and auto-reload")
	verbose    = flag.Bool("verbose", false, "verbose output")
	version    = flag.Bool("version", false, "display version")
)

const (
	fadeDuration      = 250 * time.Millisecond
	longPressDuration = 350 * time.Millisecond
)

func fatal(v ...interface{}) {
	go func() { shutdown <- errors.New(fmt.Sprint(v...)) }()
}

func fatalf(format string, a ...interface{}) {
	go func() { shutdown <- fmt.Errorf(format, a...) }()
}

func verbosef(format string, a ...interface{}) {
	if !*verbose {
		return
	}

	fmt.Printf(format+"\n", a...)
}

func expandPath(base, path string) (string, error) {
	var err error
	path, err = homedir.Expand(path)
	if err != nil {
		return "", err
	}
	if base == "" {
		return path, nil
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	return filepath.Abs(path)
}

// reloadDeck reloads the deck configuration from disk.
func reloadDeck(dev *streamdeck.Device) {
	verbosef("Reloading configuration...")

	nd, err := LoadDeck(dev, ".", deck.File)
	if err != nil {
		verbosef("The new configuration is not valid, keeping the current one.")
		fmt.Fprintf(os.Stderr, "Configuration Error: %s\n", err)
		return
	}

	InvalidateKeyImagesCache()
	deck = nd
	deck.updateWidgets()
}

// startFileWatcher watches the deck file for changes and sends on the channel.
func startFileWatcher(path string) (chan struct{}, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ch := make(chan struct{}, 1)

	go func() {
		var debounce <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					// debounce: wait 200ms after last write before triggering
					debounce = time.After(200 * time.Millisecond)
				}
			case <-debounce:
				select {
				case ch <- struct{}{}:
				default:
				}
				debounce = nil
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "File watcher error: %s\n", err)
			}
		}
	}()

	// Watch the decks directory and all immediate subdirectories that contain
	// .deck files (plugin layout: decks/<plugin>/<name>.deck).
	decksDir := filepath.Dir(path)
	if err := watcher.Add(decksDir); err != nil {
		watcher.Close()
		return nil, err
	}
	entries, _ := os.ReadDir(decksDir)
	for _, e := range entries {
		if e.IsDir() {
			subdir := filepath.Join(decksDir, e.Name())
			_ = watcher.Add(subdir) // best-effort; ignore errors for non-deck dirs
		}
	}
	verbosef("Watching %s (and plugin subdirs) for changes", decksDir)
	return ch, nil
}

// startAPI starts the HTTP API server for live key updates.
// Endpoints:
//
//	POST /key/<index>  {"label":"text", "color":"#ff0000", "icon":"/path/to/img.png"}
//	POST /reload       trigger config reload
//	GET  /health       health check
func startAPI(dev *streamdeck.Device, addr string) (chan struct{}, error) {
	reloadCh := make(chan struct{}, 1)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/screenshot", func(w http.ResponseWriter, r *http.Request) {
		px := int(dev.Pixels) // 72
		gap := 4
		cols, rows := int(dev.Columns), int(dev.Rows) // 5, 3
		totalW := cols*px + (cols-1)*gap
		totalH := rows*px + (rows-1)*gap

		grid := image.NewRGBA(image.Rect(0, 0, totalW, totalH))
		// Fill black
		for y := 0; y < totalH; y++ {
			for x := 0; x < totalW; x++ {
				grid.Set(x, y, color.RGBA{0, 0, 0, 255})
			}
		}

		// Check which keys were pressed recently (within 300ms)
		pressWindow := 300 * time.Millisecond
		now := time.Now()
		pressed := make([]bool, dev.Keys)
		keyPressedMu.RLock()
		for i := 0; i < int(dev.Keys); i++ {
			if !keyPressedAt[i].IsZero() && now.Sub(keyPressedAt[i]) < pressWindow {
				pressed[i] = true
			}
		}
		keyPressedMu.RUnlock()

		keyImagesMu.RLock()
		for idx := 0; idx < int(dev.Keys); idx++ {
			col := idx % cols
			row := idx / cols
			x0 := col * (px + gap)
			y0 := row * (px + gap)
			if keyImages[idx] != nil {
				src := keyImages[idx]
				if pressed[idx] {
					// Draw with 2px offset and white overlay for press effect
					draw.Draw(grid, image.Rect(x0+2, y0+2, x0+px+2, y0+px+2),
						src, image.Point{}, draw.Over)
					// White overlay (alpha 60)
					for py := y0 + 2; py < y0+px+2 && py < totalH; py++ {
						for px2 := x0 + 2; px2 < x0+px+2 && px2 < totalW; px2++ {
							c := grid.RGBAAt(px2, py)
							nr := int(c.R) + 60
							ng := int(c.G) + 60
							nb := int(c.B) + 60
							if nr > 255 { nr = 255 }
							if ng > 255 { ng = 255 }
							if nb > 255 { nb = 255 }
							c.R = uint8(nr)
							c.G = uint8(ng)
							c.B = uint8(nb)
							grid.SetRGBA(px2, py, c)
						}
					}
				} else {
					draw.Draw(grid, image.Rect(x0, y0, x0+px, y0+px),
						src, image.Point{}, draw.Over)
				}
			}
		}
		keyImagesMu.RUnlock()

		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, grid)
	})

	mux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		select {
		case reloadCh <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "reload triggered")
	})

	mux.HandleFunc("/key/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		idxStr := strings.TrimPrefix(r.URL.Path, "/key/")
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= int(dev.Keys) {
			http.Error(w, "invalid key index", http.StatusBadRequest)
			return
		}

		var req struct {
			Label      string  `json:"label"`
			Color      string  `json:"color"`
			Background string  `json:"background"`
			Icon       string  `json:"icon"`
			FontSize   float64 `json:"fontsize"`
			Duration   string  `json:"duration"` // e.g. "5s", "30s" — auto-revert after
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Build background image if background color specified
		var bg image.Image
		if req.Background != "" {
			clr, err := colorful.Hex(req.Background)
			if err == nil {
				px := int(dev.Pixels)
				rgba := image.NewRGBA(image.Rect(0, 0, px, px))
				for y := 0; y < px; y++ {
					for x := 0; x < px; x++ {
						rgba.Set(x, y, clr)
					}
				}
				bg = rgba
			}
		}

		// Build a temporary button widget config
		cfg := make(map[string]interface{})
		if req.Label != "" {
			cfg["label"] = req.Label
		}
		if req.Color != "" {
			cfg["color"] = req.Color
		}
		if req.Icon != "" {
			cfg["icon"] = req.Icon
		}
		if req.FontSize > 0 {
			cfg["fontsize"] = req.FontSize
		}

		wc := WidgetConfig{ID: "button", Config: cfg}
		bw := NewBaseWidget(dev, ".", uint8(idx), nil, nil, bg)
		btn, err := NewButtonWidget(bw, wc)
		if err != nil {
			http.Error(w, "widget error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := btn.Update(); err != nil {
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Replace widget in current deck
		for i, widget := range deck.Widgets {
			if widget.Key() == uint8(idx) {
				deck.Widgets[i] = btn
				break
			}
		}

		// Auto-revert after duration
		if req.Duration != "" {
			if dur, err := time.ParseDuration(req.Duration); err == nil {
				// Lock this key so the render loop won't let any widget
				// (e.g. the command widget that triggered this alert) overwrite
				// the temporary background.
				keyLocks.Store(uint8(idx), time.Now().Add(dur))

				// The script that fires the alert runs *inside* a command
				// widget's Update() call, which will render its own output
				// immediately after the script returns — overwriting the red
				// background.  Re-render after a short delay to win that race.
				go func() {
					time.Sleep(600 * time.Millisecond)
					_ = btn.Update()
				}()

				go func() {
					time.Sleep(dur)
					keyLocks.Delete(uint8(idx))
					reloadDeck(dev)
				}()
			}
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "key %d updated\n", idx)
		verbosef("API: key %d updated via HTTP", idx)
	})

	var listener net.Listener
	var listenErr error
	if strings.HasPrefix(addr, "/") {
		// Unix socket
		os.Remove(addr)
		listener, listenErr = net.Listen("unix", addr)
		if listenErr != nil {
			return nil, listenErr
		}
		os.Chmod(addr, 0660)
		verbosef("API listening on unix:%s", addr)
	} else {
		listener, listenErr = net.Listen("tcp", addr)
		if listenErr != nil {
			return nil, listenErr
		}
		verbosef("API listening on http://%s", addr)
	}

	go http.Serve(listener, mux)
	return reloadCh, nil
}

func eventLoop(dev *streamdeck.Device, tch chan interface{}) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	// File watcher (--watch)
	var fileChanged chan struct{}
	if *watch {
		var err error
		fileChanged, err = startFileWatcher(deck.File)
		if err != nil {
			fmt.Fprintf(os.Stderr, "File watcher failed: %s (falling back to SIGHUP only)\n", err)
		}
	}
	if fileChanged == nil {
		fileChanged = make(chan struct{}) // never fires
	}

	// HTTP API (--api)
	var apiReload chan struct{}
	if *apiAddr != "" {
		var err error
		apiReload, err = startAPI(dev, *apiAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "API server failed: %s\n", err)
		}
	}
	if apiReload == nil {
		apiReload = make(chan struct{}) // never fires
	}

	var keyStates sync.Map
	keyTimestamps := make(map[uint8]time.Time)

	kch, err := dev.ReadKeys()
	if err != nil {
		return err
	}
	for {
		select {
		case <-time.After(100 * time.Millisecond):
			deck.updateWidgets()

		case k, ok := <-kch:
			if !ok {
				verbosef("Key channel closed, attempting to reconnect Stream Deck...")
				backoff := 100 * time.Millisecond
				for {
					time.Sleep(backoff)
					var openErr error
					if openErr = dev.Open(); openErr == nil {
						verbosef("Stream Deck reconnected successfully.")
						_ = dev.Reset()
						_ = dev.SetBrightness(uint8(*brightness))
						dev.SetSleepFadeDuration(fadeDuration)
						if len(*sleep) > 0 {
							if timeout, openErr := time.ParseDuration(*sleep); openErr == nil {
								dev.SetSleepTimeout(timeout)
							}
						}
						if kch, openErr = dev.ReadKeys(); openErr == nil {
							InvalidateKeyImagesCache()
							deck.updateWidgets()
							break
						}
					}
					verbosef("Reconnect failed: %v. Retrying in %v...", openErr, backoff)
					backoff *= 2
					if backoff > 3*time.Second {
						backoff = 3 * time.Second
					}
				}
				continue
			}

			var state bool
			if ks, ok := keyStates.Load(k.Index); ok {
				state = ks.(bool)
			}
			keyStates.Store(k.Index, k.Pressed)

			if k.Pressed && int(k.Index) < len(keyPressedAt) {
				keyPressedMu.Lock()
				keyPressedAt[k.Index] = time.Now()
				keyPressedMu.Unlock()
			}

			if state && !k.Pressed {
				// key was released
				if time.Since(keyTimestamps[k.Index]) < longPressDuration {
					verbosef("Triggering short action for key %d", k.Index)
					deck.triggerAction(dev, k.Index, false)
				}
			}
			if !state && k.Pressed {
				// key was pressed
				go func() {
					// launch timer to observe keystate
					time.Sleep(longPressDuration)

					if state, ok := keyStates.Load(k.Index); ok && state.(bool) {
						// key still pressed
						verbosef("Triggering long action for key %d", k.Index)
						deck.triggerAction(dev, k.Index, true)
					}
				}()
			}
			keyTimestamps[k.Index] = time.Now()

		case e := <-tch:
			switch event := e.(type) {
			case WindowClosedEvent:
				handleWindowClosed(event)

			case ActiveWindowChangedEvent:
				handleActiveWindowChanged(dev, event)
			}

		case err := <-shutdown:
			return err

		case <-hup:
			verbosef("Received SIGHUP")
			reloadDeck(dev)

		case <-fileChanged:
			verbosef("Deck file changed on disk")
			reloadDeck(dev)

		case <-apiReload:
			verbosef("Reload triggered via API")
			reloadDeck(dev)

		case <-sigs:
			fmt.Println("Shutting down...")
			return nil
		}
	}
}

func closeDevice(dev *streamdeck.Device) {
	if err := dev.Reset(); err != nil {
		fmt.Fprintln(os.Stderr, "Unable to reset Stream Deck")
	}
	if err := dev.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "Unable to close Stream Deck")
	}
}

func initDevice() (*streamdeck.Device, error) {
	d, err := streamdeck.Devices()
	if err != nil {
		return nil, err
	}
	if len(d) == 0 {
		return nil, fmt.Errorf("no Stream Deck devices found")
	}

	dev := d[0]
	if len(*device) > 0 {
		found := false
		for _, v := range d {
			if v.Serial == *device {
				dev = v
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintln(os.Stderr, "Can't find device. Available devices:")
			for _, v := range d {
				fmt.Fprintf(os.Stderr, "Serial %s (%d buttons)\n", v.Serial, dev.Keys)
			}
			os.Exit(1)
		}
	}

	if err := dev.Open(); err != nil {
		return nil, err
	}
	ver, err := dev.FirmwareVersion()
	if err != nil {
		return &dev, err
	}
	verbosef("Found device with serial %s (%d buttons, firmware %s)",
		dev.Serial, dev.Keys, ver)

	if err := dev.Reset(); err != nil {
		return &dev, err
	}

	if *brightness > 100 {
		*brightness = 100
	}
	if err = dev.SetBrightness(uint8(*brightness)); err != nil {
		return &dev, err
	}

	dev.SetSleepFadeDuration(fadeDuration)
	if len(*sleep) > 0 {
		timeout, err := time.ParseDuration(*sleep)
		if err != nil {
			return &dev, err
		}

		dev.SetSleepTimeout(timeout)
	}

	return &dev, nil
}

func run() error {
	// initialize device
	dev, err := initDevice()
	if dev != nil {
		defer closeDevice(dev)
	}
	if err != nil {
		return fmt.Errorf("Unable to initialize Stream Deck: %s", err)
	}

	// initialize dbus connection
	dbusConn, err = dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("Unable to connect to dbus: %s", err)
	}

	// initialize xorg connection and track window focus
	tch := make(chan interface{})
	xorg, err = Connect(os.Getenv("DISPLAY"))
	if err == nil {
		defer xorg.Close()
		xorg.TrackWindows(tch, time.Second)
	} else {
		fmt.Fprintf(os.Stderr, "Could not connect to X server: %s\n", err)
		fmt.Fprintln(os.Stderr, "Tracking window manager will be disabled!")
	}

	// initialize virtual keyboard
	keyboard, err = uinput.CreateKeyboard("/dev/uinput", []byte("Deckmaster"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not create virtual input device (/dev/uinput): %s\n", err)
		fmt.Fprintln(os.Stderr, "Emulating keyboard events will be disabled!")
	} else {
		defer keyboard.Close() //nolint:errcheck
	}

	// load deck
	deck, err = LoadDeck(dev, ".", *deckFile)
	if err != nil {
		return fmt.Errorf("Can't load deck: %s", err)
	}
	deck.updateWidgets()

	return eventLoop(dev, tch)
}

func main() {
	flag.Parse()

	if *version {
		if len(CommitSHA) > 7 {
			CommitSHA = CommitSHA[:7]
		}
		if Version == "" {
			Version = "(built from source)"
		}

		fmt.Printf("deckmaster %s", Version)
		if len(CommitSHA) > 0 {
			fmt.Printf(" (%s)", CommitSHA)
		}

		fmt.Println()
		os.Exit(0)
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
