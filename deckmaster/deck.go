package main

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/godbus/dbus"
	"github.com/muesli/streamdeck"
)

// Deck is a set of widgets.
type Deck struct {
	File       string
	Background image.Image
	Widgets    []Widget
	gen        uint64
}

var (
	deckMu       sync.RWMutex
	deckSwitchMu sync.Mutex
)

func getActiveDeck() *Deck {
	deckMu.RLock()
	d := deck
	deckMu.RUnlock()
	return d
}

func activateDeck(d *Deck) {
	newGen := currentDeckGen.Add(1)
	d.gen = newGen
	for _, w := range d.Widgets {
		w.setDeckGen(newGen)
	}
	deckMu.Lock()
	deck = d
	deckMu.Unlock()
}

func (d *Deck) keyHasHoldAction(index uint8) bool {
	for _, w := range d.Widgets {
		if w.Key() == index {
			return w.ActionHold() != nil
		}
	}
	return false
}

// LoadDeck loads a deck configuration.
func LoadDeck(dev *streamdeck.Device, base string, deck string) (*Deck, error) {
	path, err := expandPath(base, deck)
	if err != nil {
		return nil, err
	}
	verbosef("Loading deck: %s", path)

	dc, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	d := Deck{
		File: path,
	}
	if dc.Background != "" {
		bgpath, err := expandPath(filepath.Dir(path), dc.Background)
		if err != nil {
			return nil, err
		}
		if err := d.loadBackground(dev, bgpath); err != nil {
			return nil, err
		}
	}

	keyMap := map[uint8]KeyConfig{}
	for _, k := range dc.Keys {
		keyMap[k.Index] = k
	}

	for i := uint8(0); i < dev.Keys; i++ {
		bg := d.backgroundForKey(dev, i)

		var w Widget
		if k, found := keyMap[i]; found {
			w, err = NewWidget(dev, filepath.Dir(path), k, bg)
			if err != nil {
				return nil, err
			}
		} else {
			w = NewBaseWidget(dev, filepath.Dir(path), i, nil, nil, bg)
		}

		d.Widgets = append(d.Widgets, w)
	}

	return &d, nil
}

// loads a background image.
func (d *Deck) loadBackground(dev *streamdeck.Device, bg string) error {
	f, err := os.Open(bg)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	background, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	rows := int(dev.Rows)
	cols := int(dev.Columns)
	padding := int(dev.Padding)
	pixels := int(dev.Pixels)

	width := cols*pixels + (cols-1)*padding
	height := rows*pixels + (rows-1)*padding
	if background.Bounds().Dx() != width ||
		background.Bounds().Dy() != height {
		return fmt.Errorf("supplied background image has wrong dimensions, expected %dx%d pixels", width, height)
	}

	d.Background = background
	return nil
}

// returns the background image for an individual key.
func (d Deck) backgroundForKey(dev *streamdeck.Device, key uint8) image.Image {
	padding := int(dev.Padding)
	pixels := int(dev.Pixels)
	bg := image.NewRGBA(image.Rect(0, 0, pixels, pixels))

	if d.Background != nil {
		startx := int(key%dev.Columns) * (pixels + padding)
		starty := int(key/dev.Columns) * (pixels + padding)
		draw.Draw(bg, bg.Bounds(), d.Background, image.Point{startx, starty}, draw.Src)
	}

	return bg
}

// handles keypress with delay.
func emulateKeyPressWithDelay(keys string) {
	kd := strings.Split(keys, "+")
	emulateKeyPress(kd[0])
	if len(kd) == 1 {
		return
	}

	// optional delay
	if delay, err := strconv.Atoi(strings.TrimSpace(kd[1])); err == nil {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

// emulates a range of key presses.
func emulateKeyPresses(keys string) {
	for _, kp := range strings.Split(keys, "/") {
		emulateKeyPressWithDelay(kp)
	}
}

// emulates a (multi-)key press.
func emulateKeyPress(keys string) {
	if keyboard == nil {
		fmt.Fprintln(os.Stderr, "Keyboard emulation is disabled!")
		return
	}

	kk := strings.Split(keys, "-")
	for i, k := range kk {
		k = formatKeycodes(strings.TrimSpace(k))
		kc, err := strconv.Atoi(k)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s is not a valid keycode: %s\n", k, err)
		}

		if i+1 < len(kk) {
			_ = keyboard.KeyDown(kc)
			defer keyboard.KeyUp(kc) //nolint:errcheck
		} else {
			_ = keyboard.KeyPress(kc)
		}
	}
}

// emulates a clipboard paste.
func emulateClipboard(text string) {
	err := clipboard.WriteAll(text)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Pasting to clipboard failed: %s\n", err)
	}

	// paste the string
	emulateKeyPress("29-47") // ctrl-v
}

// executes a dbus method.
func executeDBusMethod(object, path, method, args string) {
	call := dbusConn.Object(object, dbus.ObjectPath(path)).Call(method, 0, args)
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "dbus call failed: %s\n", call.Err)
	}
}

func buildExecCommand(cmd string) *exec.Cmd {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return nil
	}
	if strings.ContainsAny(trimmed, ";|&><\"'$`") {
		return exec.Command("sh", "-c", trimmed) //nolint:gosec
	}
	exp, err := expandPath("", trimmed)
	if err == nil {
		trimmed = exp
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil
	}
	return exec.Command(fields[0], fields[1:]...) //nolint:gosec
}

// executes a command.
func executeCommand(cmd string) {
	c := buildExecCommand(cmd)
	if c == nil {
		return
	}
	if *verbose {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	}

	if err := c.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Command failed: %s\n", err)
		return
	}
	if err := c.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "Command failed: %s\n", err)
	}
}

// triggerAction triggers an action.
func (d *Deck) triggerAction(dev *streamdeck.Device, index uint8, hold bool) {
	for _, w := range d.Widgets {
		if w.Key() != index {
			continue
		}

		var a *ActionConfig
		if hold {
			a = w.ActionHold()
		} else {
			a = w.Action()
		}

		if a == nil {
			w.TriggerAction(hold)
			continue
		}

		if a.Exec != "" && a.Deck != "" {
			// Run exec synchronously BEFORE deck switch so state is set
			executeCommand(a.Exec)
		}
		if a.Deck != "" {
			deckSwitchMu.Lock()
			nd, err := LoadDeck(dev, filepath.Dir(d.File), a.Deck)
			if err != nil {
				deckSwitchMu.Unlock()
				fmt.Fprintln(os.Stderr, "Can't load deck:", err)
				return
			}
			if err := dev.Clear(); err != nil {
				deckSwitchMu.Unlock()
				fatal(err)
				return
			}

			time.Sleep(5 * time.Millisecond) // Give microcontroller a brief moment to clear buffers
			InvalidateKeyImagesCache()
			activateDeck(nd)
			nd.forceUpdateWidgets()
			deckSwitchMu.Unlock()
		}
		if a.Keycode != "" {
			emulateKeyPresses(a.Keycode)
		}
		if a.Paste != "" {
			emulateClipboard(a.Paste)
		}
		if a.DBus.Method != "" {
			executeDBusMethod(a.DBus.Object, a.DBus.Path, a.DBus.Method, a.DBus.Value)
		}
		if a.Exec != "" && a.Deck == "" {
			currentWidget := w
			deckGen := d.gen
			go func() {
				executeCommand(a.Exec)
				if currentDeckGen.Load() != deckGen {
					return
				}
				if currentWidget.tryStartUpdate() {
					currentWidget.ResetUpdate()
					if err := currentWidget.Update(); err != nil {
						fmt.Fprintf(os.Stderr, "widget %d update error: %v\n", currentWidget.Key(), err)
					}
					currentWidget.finishUpdate()
				}

				// Follow-up refresh for eventual-consistency backends (e.g. Home Assistant).
				go func() {
					time.Sleep(500 * time.Millisecond)
					if currentDeckGen.Load() != deckGen {
						return
					}
					if currentWidget.tryStartUpdate() {
						currentWidget.ResetUpdate()
						if err := currentWidget.Update(); err != nil {
							fmt.Fprintf(os.Stderr, "widget %d update error: %v\n", currentWidget.Key(), err)
						}
						currentWidget.finishUpdate()
					}
				}()
			}()
		}
		if a.Device != "" {
			switch {
			case a.Device == "sleep":
				if err := dev.Sleep(); err != nil {
					fatalf("error: %v\n", err)
				}

			case strings.HasPrefix(a.Device, "brightness"):
				d.adjustBrightness(dev, strings.TrimPrefix(a.Device, "brightness"))

			default:
				fmt.Fprintln(os.Stderr, "Unrecognized special action:", a.Device)
			}
		}
	}
}

// forceUpdateWidgets resets the update timer on all widgets and launches non-blocking updates.
func (d *Deck) forceUpdateWidgets() {
	now := time.Now()
	for _, w := range d.Widgets {
		w.ResetUpdate()

		// Skip keys locked by an API override (e.g. an alert flash).
		if until, ok := keyLocks.Load(w.Key()); ok && now.Before(until.(time.Time)) {
			continue
		}

		if !w.tryStartUpdate() {
			continue
		}

		go func(widget Widget) {
			defer widget.finishUpdate()
			if err := widget.Update(); err != nil {
				fmt.Fprintf(os.Stderr, "widget %d update error: %v\n", widget.Key(), err)
			}
		}(w)
	}
}

// updateWidgets updates/repaints all widgets that require an update without blocking the caller.
func (d *Deck) updateWidgets() {
	now := time.Now()
	for _, w := range d.Widgets {
		// Skip keys locked by an API override (e.g. an alert flash).
		if until, ok := keyLocks.Load(w.Key()); ok && now.Before(until.(time.Time)) {
			continue
		}

		if !w.RequiresUpdate() || !w.tryStartUpdate() {
			continue
		}

		go func(widget Widget) {
			defer widget.finishUpdate()
			if err := widget.Update(); err != nil {
				fmt.Fprintf(os.Stderr, "widget %d update error: %v\n", widget.Key(), err)
			}
		}(w)
	}
}

// adjustBrightness adjusts the brightness.
func (d *Deck) adjustBrightness(dev *streamdeck.Device, value string) {
	if len(value) == 0 {
		fmt.Fprintln(os.Stderr, "No brightness value specified")
		return
	}

	v := int64(math.MinInt64)
	if len(value) > 1 {
		nv, err := strconv.ParseInt(value[1:], 10, 64)
		if err == nil {
			v = nv
		}
	}

	switch value[0] {
	case '=': // brightness=[n]:
	case '-': // brightness-[n]:
		if v == math.MinInt64 {
			v = 10
		}
		v = int64(*brightness) - v
	case '+': // brightness+[n]:
		if v == math.MinInt64 {
			v = 10
		}
		v = int64(*brightness) + v
	default:
		v = math.MinInt64
	}

	if v == math.MinInt64 {
		fmt.Fprintf(os.Stderr, "Could not grok the brightness from value '%s'\n", value)
		return
	}

	if v < 1 {
		v = 1
	} else if v > 100 {
		v = 100
	}
	if err := dev.SetBrightness(uint8(v)); err != nil {
		fatalf("error: %v\n", err)
	}

	*brightness = uint(v)
}
