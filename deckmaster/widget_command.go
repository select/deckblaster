package main

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/mitchellh/go-homedir"
)

// CommandWidget is a widget displaying the output of command(s).
type CommandWidget struct {
	*BaseWidget

	commands []string
	fonts    []string
	frames   []image.Rectangle
	colors   []color.Color
	iconPath string // static icon path
	iconCmd  string // command that returns icon path (dynamic)
	colorCmd string // command that returns semicolon-separated hex colors (dynamic)

	iconCacheMu     sync.Mutex
	lastIconPath    string
	lastIconModTime time.Time
	lastIconSize    int64
	cachedImg       *image.RGBA
}

// NewCommandWidget returns a new CommandWidget.
func NewCommandWidget(bw *BaseWidget, opts WidgetConfig) *CommandWidget {
	bw.setInterval(time.Duration(opts.Interval)*time.Millisecond, time.Second)

	var rawCommands, rawFonts, rawFrameReps []string
	_ = ConfigValue(opts.Config["command"], &rawCommands)
	_ = ConfigValue(opts.Config["font"], &rawFonts)
	_ = ConfigValue(opts.Config["layout"], &rawFrameReps)
	var rawColors []color.Color
	_ = ConfigValue(opts.Config["color"], &rawColors)

	var commands, fonts, frameReps []string
	var colors []color.Color
	for i, cmd := range rawCommands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		commands = append(commands, cmd)
		if i < len(rawFonts) {
			fonts = append(fonts, rawFonts[i])
		}
		if i < len(rawColors) {
			colors = append(colors, rawColors[i])
		}
		if i < len(rawFrameReps) {
			frameReps = append(frameReps, rawFrameReps[i])
		}
	}

	var frames []image.Rectangle
	if len(commands) > 0 {
		layout := NewLayout(int(bw.dev.Pixels))
		frames = layout.FormatLayout(frameReps, len(commands))

		for i := 0; i < len(commands); i++ {
			if len(fonts) < i+1 {
				fonts = append(fonts, "regular")
			}
			if len(colors) < i+1 {
				colors = append(colors, DefaultColor)
			}
		}
	}

	w := &CommandWidget{
		BaseWidget: bw,
		commands:   commands,
		fonts:      fonts,
		frames:     frames,
		colors:     colors,
	}

	// Static icon
	var iconPath string
	_ = ConfigValue(opts.Config["icon"], &iconPath)
	if iconPath != "" {
		path, err := expandPath(bw.base, iconPath)
		if err == nil {
			w.iconPath = path
		}
	}

	// Dynamic icon: command that returns a path to a PNG
	var iconCmd string
	_ = ConfigValue(opts.Config["icon_command"], &iconCmd)
	if iconCmd != "" {
		w.iconCmd = iconCmd
	}

	// Dynamic colors: command that returns semicolon-separated hex colors
	var colorCmd string
	_ = ConfigValue(opts.Config["color_command"], &colorCmd)
	if colorCmd != "" {
		w.colorCmd = colorCmd
	}

	return w
}

// resolveIconPath returns the active icon path, preferring dynamic icon_command over static icon.
func (w *CommandWidget) resolveIconPath() string {
	path := w.iconPath
	if w.iconCmd != "" {
		if out, err := runCommand(w.iconCmd); err == nil && out != "" {
			path = out
		}
	}
	if strings.HasPrefix(path, "~") {
		if expanded, err := homedir.Expand(path); err == nil {
			path = expanded
		}
	}
	return path
}

// loadCurrentIcon loads the icon, preferring dynamic icon_command over static icon.
func (w *CommandWidget) loadCurrentIcon() image.Image {
	path := w.resolveIconPath()
	if path == "" {
		return nil
	}

	img, err := loadImage(path)
	if err != nil {
		return nil
	}
	return img
}

// Update renders the widget.
func (w *CommandWidget) Update() error {
	size := int(w.dev.Pixels)

	// Fast path for icon-only command widgets: stat cache avoids re-opening and decoding unchanged PNGs.
	if len(w.commands) == 0 && w.colorCmd == "" {
		path := w.resolveIconPath()
		if path == "" {
			return w.render(w.dev, image.NewRGBA(image.Rect(0, 0, size, size)))
		}
		if info, statErr := os.Stat(path); statErr == nil {
			w.iconCacheMu.Lock()
			if w.cachedImg != nil &&
				w.lastIconPath == path &&
				w.lastIconModTime.Equal(info.ModTime()) &&
				w.lastIconSize == info.Size() {
				cached := w.cachedImg
				w.iconCacheMu.Unlock()
				return w.render(w.dev, cached)
			}
			w.iconCacheMu.Unlock()

			if icon, err := loadImage(path); err == nil && icon != nil {
				img := image.NewRGBA(image.Rect(0, 0, size, size))
				_ = drawImage(img, icon, size, image.Pt(0, 0))
				w.iconCacheMu.Lock()
				w.lastIconPath = path
				w.lastIconModTime = info.ModTime()
				w.lastIconSize = info.Size()
				w.cachedImg = img
				w.iconCacheMu.Unlock()
				return w.render(w.dev, img)
			}
		}
		return w.render(w.dev, image.NewRGBA(image.Rect(0, 0, size, size)))
	}

	margin := size / 18
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Load icon (static or dynamic)
	icon := w.loadCurrentIcon()

	textTop := 0
	if icon != nil {
		hasText := len(w.commands) > 0
		if hasText {
			iconSize := size / 2
			_ = drawImage(img, icon, iconSize, image.Pt(-1, margin))
			textTop = iconSize + margin
		} else {
			// No text — icon fills full button
			_ = drawImage(img, icon, size, image.Pt(0, 0))
		}
	}

	// Resolve dynamic colors if color_command is set
	colors := w.colors
	if w.colorCmd != "" {
		if out, err := runCommand(w.colorCmd); err == nil && out != "" {
			parts := strings.Split(out, ";")
			dynColors := make([]color.Color, len(parts))
			for j, p := range parts {
				c, cErr := colorful.Hex(strings.TrimSpace(p))
				if cErr == nil {
					dynColors[j] = c
				} else if j < len(w.colors) {
					dynColors[j] = w.colors[j]
				} else {
					dynColors[j] = DefaultColor
				}
			}
			colors = dynColors
		}
	}

	for i := 0; i < len(w.commands); i++ {
		str, err := runCommand(w.commands[i])
		if err != nil {
			return err
		}
		font := fontByName(w.fonts[i])

		frame := w.frames[i]
		if icon != nil {
			remaining := size - textTop
			frameH := remaining / len(w.commands)
			frame = image.Rect(0, textTop+frameH*i, size, textTop+frameH*(i+1))
		}

		clr := colors[i]
		if i >= len(colors) {
			clr = DefaultColor
		}

		drawString(img,
			frame,
			font,
			str,
			w.dev.DPI,
			-1,
			clr,
			image.Pt(-1, -1))
	}
	return w.render(w.dev, img)
}

func runCommand(command string) (string, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return "", nil
	}

	if strings.HasPrefix(trimmed, "echo ") {
		arg := strings.TrimSpace(strings.TrimPrefix(trimmed, "echo"))
		// Strip wrapping double or single quotes if present
		if (strings.HasPrefix(arg, "\"") && strings.HasSuffix(arg, "\"")) ||
			(strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'")) {
			arg = arg[1 : len(arg)-1]
		}
		// If there are no shell metacharacters, we can safely return the string directly!
		if !strings.ContainsAny(arg, "|$&;><`\\*?()!") {
			return arg, nil
		}
	}

	if !strings.ContainsAny(trimmed, "|$&;><`\\*?()!~'\"\n") {
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			return "", nil
		}
		output, err := exec.Command(fields[0], fields[1:]...).Output() //nolint:gosec
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(output), "\n"), nil
	}

	output, err := exec.Command("sh", "-c", command).Output() //nolint:gosec
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}
