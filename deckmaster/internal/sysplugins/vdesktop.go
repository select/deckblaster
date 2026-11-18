package sysplugins

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nfnt/resize"
)

const (
	vdesktopOutDir       = "/tmp/streamdeck-vdesktop"
	vdesktopIconCacheDir = "/tmp/streamdeck-icon-cache"
	vdesktopWMJSON       = "/tmp/streamdeck-wm.json"
	vdesktopMaxApps      = 5
	vdesktopButtonSize   = 72
)

var (
	wmOnce     sync.Once
	wmDetected string

	iconMapOnce sync.Once
	iconMapData map[string]string

	nonAlnumRe = regexp.MustCompile(`[^a-zA-Z0-9]`)
	nonLowerRe = regexp.MustCompile(`[^a-z0-9]`)
	xpropClsRe = regexp.MustCompile(`"([^"]*)",\s*"([^"]*)"`)
)

func ensureHyprlandEnv() {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return
	}
	uid := os.Getuid()
	hyprDir := fmt.Sprintf("/run/user/%d/hypr", uid)
	entries, err := os.ReadDir(hyprDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lockPath := filepath.Join(hyprDir, entry.Name(), "hyprland.lock")
		data, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		lines := strings.SplitN(string(data), "\n", 2)
		pidStr := strings.TrimSpace(lines[0])
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			continue
		}
		if strings.Contains(string(comm), "Hyprland") {
			_ = os.Setenv("HYPRLAND_INSTANCE_SIGNATURE", entry.Name())
			return
		}
	}
}

func getWindowManager() string {
	wmOnce.Do(func() {
		ensureHyprlandEnv()
		if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
			if out, err := exec.Command("hyprctl", "activeworkspace", "-j").Output(); err == nil && len(out) > 0 {
				wmDetected = "hyprland"
				return
			}
		}
		desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
		sessType := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
		if strings.Contains(desktop, "gnome") && sessType == "wayland" {
			wmDetected = "gnome_wayland"
			return
		}
		wmDetected = "x11"
	})
	return wmDetected
}

func checkExtensionNeeded() bool {
	if getWindowManager() != "gnome_wayland" {
		return false
	}
	st, err := os.Stat(vdesktopWMJSON)
	if err != nil {
		return true
	}
	return time.Since(st.ModTime()) >= 10*time.Second
}

// ── Icon resolution ───────────────────────────────────────────────────────────

func defaultAppIconPath() string {
	p1 := assetPath(filepath.Join("assets", "app-default.png"))
	if fileExists(p1) {
		return p1
	}
	return assetPath(filepath.Join("vdesktop", "assets", "app-default.png"))
}

func buildIconSearchIndex() map[string]string {
	// Build a fast index of icon_stem -> path from standard icon dirs
	idx := make(map[string]string)
	home, _ := os.UserHomeDir()

	// Ordered from highest preference to lowest (first wins per stem, except PNG preferred over SVG)
	themeRoots := []string{
		filepath.Join(home, ".local/share/icons/hicolor"),
		"/usr/share/icons/hicolor",
		"/usr/share/icons/Papirus",
		"/usr/share/icons/Adwaita",
	}
	sizes := []string{"64x64", "128x128", "48x48", "256x256", "32x32", "scalable"}

	addFile := func(stem, path string) {
		stem = strings.ToLower(stem)
		if existing, ok := idx[stem]; ok {
			// Prefer PNG over SVG
			if strings.HasSuffix(existing, ".svg") && strings.HasSuffix(path, ".png") {
				idx[stem] = path
			}
			return
		}
		idx[stem] = path
	}

	for _, root := range themeRoots {
		for _, sz := range sizes {
			appsDir := filepath.Join(root, sz, "apps")
			entries, err := os.ReadDir(appsDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				ext := strings.ToLower(filepath.Ext(name))
				if ext != ".png" && ext != ".svg" {
					continue
				}
				stem := strings.TrimSuffix(name, ext)
				addFile(stem, filepath.Join(appsDir, name))
			}
		}
	}

	if entries, err := os.ReadDir("/usr/share/pixmaps"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".png" && ext != ".svg" {
				continue
			}
			stem := strings.TrimSuffix(name, ext)
			addFile(stem, filepath.Join("/usr/share/pixmaps", name))
		}
	}

	return idx
}

func buildIconMap() map[string]string {
	m := make(map[string]string)
	home, _ := os.UserHomeDir()

	manual := map[string]string{
		"chromium":        "/snap/chromium/current/chromium.png",
		"dev.zed.zed":     filepath.Join(home, ".local/zed.app/share/icons/hicolor/512x512/apps/zed.png"),
		"firefox_firefox": "/usr/share/icons/hicolor/128x128/apps/firefox.png",
		"navigator":       "/usr/share/icons/hicolor/128x128/apps/firefox.png",
		"kitty":           "/usr/share/icons/hicolor/256x256/apps/kitty.png",
	}
	for k, v := range manual {
		if fileExists(v) {
			m[k] = v
		}
	}

	themeIdx := buildIconSearchIndex()

	resolveIcon := func(iconName string) string {
		if iconName == "" {
			return ""
		}
		if strings.HasPrefix(iconName, "/") && fileExists(iconName) {
			return iconName
		}
		key := strings.ToLower(strings.TrimSuffix(iconName, filepath.Ext(iconName)))
		if p, ok := themeIdx[key]; ok {
			return p
		}
		return ""
	}

	desktopDirs := []string{
		"/usr/share/applications",
		filepath.Join(home, ".local/share/applications"),
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
	}

	for _, d := range desktopDirs {
		files, err := filepath.Glob(filepath.Join(d, "*.desktop"))
		if err != nil {
			continue
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			props := make(map[string]string)
			inMain := true
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "[") && strings.TrimSpace(line) != "[Desktop Entry]" {
					inMain = false
				}
				if inMain {
					if idx := strings.IndexByte(line, '='); idx > 0 {
						k := strings.TrimSpace(line[:idx])
						v := strings.TrimSpace(line[idx+1:])
						props[k] = v
					}
				}
			}

			iconPath := resolveIcon(props["Icon"])
			if iconPath == "" {
				continue
			}

			var keys []string
			if wmc := strings.ToLower(strings.TrimSpace(props["StartupWMClass"])); wmc != "" {
				keys = append(keys, wmc)
			}
			base := filepath.Base(f)
			stem := strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base)))
			keys = append(keys, stem)
			if parts := strings.Split(stem, "."); len(parts) > 1 {
				keys = append(keys, parts[len(parts)-1])
			}
			if execVal := props["Exec"]; execVal != "" {
				fields := strings.Fields(execVal)
				if len(fields) > 0 {
					bin := strings.ToLower(filepath.Base(fields[0]))
					bin = nonLowerRe.ReplaceAllString(bin, "")
					if bin != "" {
						keys = append(keys, bin)
					}
				}
			}
			if name := strings.ToLower(strings.TrimSpace(props["Name"])); name != "" {
				keys = append(keys, name)
				if clean := nonLowerRe.ReplaceAllString(name, ""); clean != "" {
					keys = append(keys, clean)
				}
			}

			for _, key := range keys {
				if key != "" {
					if _, exists := m[key]; !exists {
						m[key] = iconPath
					}
				}
			}
		}
	}

	// Add theme icons directly as fallback keys
	for k, p := range themeIdx {
		if _, exists := m[k]; !exists {
			m[k] = p
		}
	}

	// Bundled vdesktop/assets/app-*.png fallback
	if bundled, err := filepath.Glob(assetPath(filepath.Join("vdesktop", "assets", "app-*.png"))); err == nil {
		for _, p := range bundled {
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".png"))
			name := strings.TrimPrefix(base, "app-")
			if name != "" && name != "default" {
				if _, exists := m[name]; !exists {
					m[name] = p
				}
			}
		}
	}

	return m
}

func getIconMap() map[string]string {
	iconMapOnce.Do(func() {
		iconMapData = buildIconMap()
	})
	return iconMapData
}

func getIconPath(wmClass string) string {
	m := getIconMap()
	cls := strings.ToLower(strings.TrimSpace(wmClass))
	if cls == "" {
		return defaultAppIconPath()
	}

	// 1. Exact match
	if p, ok := m[cls]; ok {
		return p
	}

	// 2. Substring match (deterministic order)
	keys := make([]string, 0, len(m))
	for k := range m {
		if len(k) >= 3 && (strings.Contains(cls, k) || strings.Contains(k, cls)) {
			keys = append(keys, k)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return m[keys[0]]
	}

	// 3. Token match
	for _, part := range strings.FieldsFunc(cls, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	}) {
		if len(part) >= 3 {
			if p, ok := m[part]; ok {
				return p
			}
		}
	}

	// 4. Dash/dotted variants
	if parts := strings.Split(cls, "."); len(parts) > 1 {
		if p, ok := m[parts[len(parts)-1]]; ok {
			return p
		}
	}
	if p, ok := m[strings.ReplaceAll(cls, "_", "-")]; ok {
		return p
	}

	return defaultAppIconPath()
}

func trimAlpha(img image.Image) image.Image {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a > 0 {
				found = true
				if x < minX {
					minX = x
				}
				if x >= maxX {
					maxX = x + 1
				}
				if y < minY {
					minY = y
				}
				if y >= maxY {
					maxY = y + 1
				}
			}
		}
	}
	if !found {
		return img
	}
	rect := image.Rect(minX, minY, maxX, maxY)
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out
}

func rasterizeSVGToCache(svgPath, cachePNG string, size int) error {
	if _, err := exec.LookPath("rsvg-convert"); err == nil {
		cmd := exec.Command("rsvg-convert", "-w", strconv.Itoa(size*2), "-h", strconv.Itoa(size*2), "-f", "png", "-o", cachePNG, svgPath)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return exec.Command("convert", "-background", "none", svgPath, "-resize", fmt.Sprintf("%dx%d", size*2, size*2), "PNG32:"+cachePNG).Run()
}

func cachedIconImage(wmClass string, size int) image.Image {
	path := cachedIcon(wmClass, size)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func cachedIcon(wmClass string, size int) string {
	raw := getIconPath(wmClass)
	if raw == "" || !fileExists(raw) {
		return defaultAppIconPath()
	}

	_ = os.MkdirAll(vdesktopIconCacheDir, 0o755)
	cacheKey := nonAlnumRe.ReplaceAllString(wmClass, "_") + fmt.Sprintf("_%d.png", size)
	cachePath := filepath.Join(vdesktopIconCacheDir, cacheKey)

	rawSt, err1 := os.Stat(raw)
	cacheSt, err2 := os.Stat(cachePath)
	if err1 == nil && err2 == nil && !rawSt.ModTime().After(cacheSt.ModTime()) {
		return cachePath
	}

	var srcImg image.Image
	if strings.EqualFold(filepath.Ext(raw), ".svg") {
		tmpPNG := filepath.Join(vdesktopIconCacheDir, nonAlnumRe.ReplaceAllString(wmClass, "_")+"_svg.png")
		if err := rasterizeSVGToCache(raw, tmpPNG, size); err != nil {
			return defaultAppIconPath()
		}
		if f, err := os.Open(tmpPNG); err == nil {
			srcImg, _, _ = image.Decode(f)
			_ = f.Close()
		}
		_ = os.Remove(tmpPNG)
	} else {
		if f, err := os.Open(raw); err == nil {
			srcImg, _, _ = image.Decode(f)
			_ = f.Close()
		}
	}

	if srcImg == nil {
		return defaultAppIconPath()
	}

	trimmed := trimAlpha(srcImg)
	tb := trimmed.Bounds()
	tw, th := tb.Dx(), tb.Dy()
	if tw <= 0 || th <= 0 {
		return defaultAppIconPath()
	}

	// Resize preserving aspect ratio within size x size
	newW, newH := size, size
	if tw > th {
		newH = maxInt(1, int(float64(th)*float64(size)/float64(tw)))
	} else if th > tw {
		newW = maxInt(1, int(float64(tw)*float64(size)/float64(th)))
	}
	resized := resize.Resize(uint(newW), uint(newH), trimmed, resize.Bilinear)

	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	ox := (size - newW) / 2
	oy := (size - newH) / 2
	draw.Draw(canvas, image.Rect(ox, oy, ox+newW, oy+newH), resized, image.Point{}, draw.Over)

	if err := atomicWritePNG(cachePath, canvas); err != nil {
		return defaultAppIconPath()
	}
	return cachePath
}

// ── Window / Desktop info ─────────────────────────────────────────────────────

func desktopsFromHyprClients(clients []hyprClient) map[int][]string {
	desktops := make(map[int][]string)
	for _, c := range clients {
		if !c.Mapped {
			continue
		}
		desk := c.Workspace.ID - 1
		if desk < 0 || desk >= 5 {
			continue
		}
		cls := strings.ToLower(strings.TrimSpace(c.Class))
		if cls == "" {
			continue
		}
		exists := false
		for _, a := range desktops[desk] {
			if a == cls {
				exists = true
				break
			}
		}
		if !exists {
			desktops[desk] = append(desktops[desk], cls)
		}
	}
	return desktops
}

func getCurrentDesktopHyprland() int {
	out, err := hyprctl("activeworkspace", "-j")
	if err != nil || out == "" {
		return 0
	}
	var obj struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil || obj.ID <= 0 {
		return 0
	}
	return obj.ID - 1
}

func getWindowsWayland() (map[int][]string, int, int) {
	desktops := make(map[int][]string)
	raw, err := os.ReadFile(vdesktopWMJSON)
	if err != nil {
		return desktops, 0, 0
	}
	var data struct {
		Desktops map[string][]string `json:"desktops"`
		Active   int                 `json:"active"`
		N        int                 `json:"n"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return desktops, 0, 0
	}
	for k, apps := range data.Desktops {
		if idx, err := strconv.Atoi(k); err == nil {
			desktops[idx] = apps
		}
	}
	return desktops, data.Active, data.N
}

func getWindowsX11() (map[int][]string, int, int) {
	desktops := make(map[int][]string)
	widsOut, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "").Output()
	for _, wid := range strings.Split(string(widsOut), "\n") {
		wid = strings.TrimSpace(wid)
		if wid == "" {
			continue
		}
		deskOut, err := exec.Command("xdotool", "get_desktop_for_window", wid).Output()
		if err != nil {
			continue
		}
		desk, err := strconv.Atoi(strings.TrimSpace(string(deskOut)))
		if err != nil {
			continue
		}
		clsOut, err := exec.Command("xprop", "-id", wid, "WM_CLASS").Output()
		if err != nil {
			continue
		}
		m := xpropClsRe.FindStringSubmatch(string(clsOut))
		if len(m) < 3 {
			continue
		}
		cls := strings.ToLower(m[2])
		exists := false
		for _, a := range desktops[desk] {
			if a == cls {
				exists = true
				break
			}
		}
		if !exists {
			desktops[desk] = append(desktops[desk], cls)
		}
	}
	cur := 0
	if curOut, err := exec.Command("xdotool", "get_desktop").Output(); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(curOut))); err == nil {
			cur = v
		}
	}
	num := 0
	if numOut, err := exec.Command("xdotool", "get_num_desktops").Output(); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(string(numOut))); err == nil {
			num = v
		}
	}
	return desktops, cur, num
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func renderDesktopImage(apps []string, current bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, vdesktopButtonSize, vdesktopButtonSize))
	n := minInt(len(apps), vdesktopMaxApps)

	if n == 0 {
		fillRectSrc(img, 0, 0, vdesktopButtonSize, vdesktopButtonSize, color.RGBA{0x3a, 0x3a, 0x3a, 255})
		if current {
			fillRectSrc(img, 0, vdesktopButtonSize-4, vdesktopButtonSize, vdesktopButtonSize, color.RGBA{0x18, 0xbc, 0xf2, 255})
		}
		return img
	}

	fillRectSrc(img, 0, 0, vdesktopButtonSize, vdesktopButtonSize, color.RGBA{0x1a, 0x1a, 0x1a, 255})

	pad := maxInt(1, (vdesktopButtonSize*5)/100)
	available := vdesktopButtonSize - 2*pad
	const gap = 2
	iconSize := available
	if n > 1 {
		iconSize = (available - (n-1)*gap) / n
	}
	totalW := n*iconSize + (n-1)*gap
	startX := (vdesktopButtonSize - totalW) / 2
	startY := (vdesktopButtonSize - iconSize) / 2

	for i := 0; i < n; i++ {
		ic := cachedIconImage(apps[i], iconSize)
		if ic != nil {
			x := startX + i*(iconSize+gap)
			draw.Draw(img, image.Rect(x, startY, x+iconSize, startY+iconSize), ic, image.Point{}, draw.Over)
		}
	}

	if current {
		fillRectSrc(img, 0, vdesktopButtonSize-4, vdesktopButtonSize, vdesktopButtonSize, color.RGBA{0x18, 0xbc, 0xf2, 255})
	}
	return img
}

func renderDesktop(deskNum int, apps []string, current bool) (string, error) {
	_ = os.MkdirAll(vdesktopOutDir, 0o755)
	outPath := filepath.Join(vdesktopOutDir, fmt.Sprintf("desk-%d.png", deskNum))
	img := renderDesktopImage(apps, current)
	if err := atomicWritePNG(outPath, img); err != nil {
		return "", err
	}
	return outPath, nil
}

type desktopSlotState struct {
	appsKey string
	current bool
	valid   bool
}

func renderAllDesktops(prev *[5]desktopSlotState, prevSlotSources *[5]string, printJSON bool) {
	_ = os.MkdirAll(vdesktopOutDir, 0o755)

	if checkExtensionNeeded() {
		installIcon := assetPath(filepath.Join("vdesktop", "assets", "install-ext.png"))
		outPath := filepath.Join(vdesktopOutDir, "desk-0.png")
		_ = atomicCopyFile(installIcon, outPath)
		_ = atomicCopyFile(installIcon, filepath.Join(vdesktopOutDir, "slot-0.png"))
		for i := 1; i < 5; i++ {
			_ = os.Remove(filepath.Join(vdesktopOutDir, fmt.Sprintf("desk-%d.png", i)))
			_ = os.Remove(filepath.Join(vdesktopOutDir, fmt.Sprintf("slot-%d.png", i)))
		}
		if printJSON {
			fmt.Printf(`{"0": {"image": %q, "apps": [], "current": false}}`+"\n", outPath)
		}
		return
	}

	wm := getWindowManager()
	var (
		windows     map[int][]string
		current     int
		numDesktops int
		hyprClients []hyprClient
	)

	switch wm {
	case "hyprland":
		hyprClients, _ = hyprctlClients()
		windows = desktopsFromHyprClients(hyprClients)
		current = getCurrentDesktopHyprland()
		numDesktops = 5
	case "gnome_wayland":
		windows, current, numDesktops = getWindowsWayland()
	default:
		windows, current, numDesktops = getWindowsX11()
	}

	if numDesktops == 0 {
		return
	}

	type jsonEntry struct {
		Image   string   `json:"image"`
		Apps    []string `json:"apps"`
		Current bool     `json:"current"`
	}
	result := make(map[string]jsonEntry, numDesktops)

	for d := 0; d < numDesktops; d++ {
		apps := windows[d]
		if apps == nil {
			apps = []string{}
		}
		isCur := d == current
		outPath := filepath.Join(vdesktopOutDir, fmt.Sprintf("desk-%d.png", d))

		changed := true
		if prev != nil && d < len(prev) {
			key := strings.Join(apps, "\x00")
			if prev[d].valid && prev[d].appsKey == key && prev[d].current == isCur && fileExists(outPath) {
				changed = false
			} else {
				prev[d] = desktopSlotState{appsKey: key, current: isCur, valid: true}
			}
		}

		if changed {
			_, _ = renderDesktop(d, apps, isCur)
		}
		if printJSON {
			result[strconv.Itoa(d)] = jsonEntry{
				Image:   outPath,
				Apps:    apps,
				Current: isCur,
			}
		}
	}

	updateMainRowSlotsFromClients(hyprClients, prevSlotSources)

	if printJSON {
		if b, err := json.Marshal(result); err == nil {
			fmt.Println(string(b))
		}
	}
}

func switchDesktop(nStr string) int {
	wm := getWindowManager()
	if wm == "gnome_wayland" && checkExtensionNeeded() {
		installScript := assetPath(filepath.Join("vdesktop", "install-extension.sh"))
		_ = exec.Command("bash", installScript).Run()
		return 0
	}

	n, err := strconv.Atoi(nStr)
	if err != nil {
		n = 0
	}

	switch wm {
	case "hyprland":
		_, _ = hyprctl("dispatch", fmt.Sprintf("hl.dsp.focus({workspace=%d})", n+1))
	case "gnome_wayland":
		_ = exec.Command("gdbus", "call", "--session",
			"--dest", "org.gnome.Shell",
			"--object-path", "/org/gnome/Shell/Extensions/DeckblasterWM",
			"--method", "org.gnome.Shell.Extensions.DeckblasterWM.SwitchWorkspace",
			strconv.Itoa(n)).Run()
	default:
		_ = exec.Command("xdotool", "set_desktop", strconv.Itoa(n)).Run()
	}
	renderAllDesktops(nil, nil, false)
	return 0
}

// RunVDesktop is the entrypoint for `./deckctl vdesktop <render|poll|switch N>`.
func RunVDesktop(args []string) int {
	cmd := "render"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "switch":
		n := "0"
		if len(args) > 1 {
			n = args[1]
		}
		return switchDesktop(n)

	case "poll":
		var prev [5]desktopSlotState
		var prevSlots [5]string
		for {
			renderAllDesktops(&prev, &prevSlots, false)
			time.Sleep(3 * time.Second)
		}

	case "render":
		renderAllDesktops(nil, nil, true)
		return 0

	default:
		printfErr("usage: vdesktop [render|poll|switch <n>]\n")
		return 1
	}
}
