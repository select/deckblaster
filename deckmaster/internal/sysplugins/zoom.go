package sysplugins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	zoomStateFile = "/tmp/streamdeck-zoom-state"
	focusSettle   = 60 * time.Millisecond
)

type hyprClient struct {
	Address   string `json:"address"`
	Class     string `json:"class"`
	Title     string `json:"title"`
	Mapped    bool   `json:"mapped"`
	Workspace struct {
		ID int `json:"id"`
	} `json:"workspace"`
}

var nonMeetingTitleMarkers = []string{
	"zoom workplace",
	"zoom cloud meetings",
	"settings",
}

type zoomIconSpec struct {
	on        string
	off       string
	noMeeting string
	field     string
}

var zoomIcons = map[string]zoomIconSpec{
	"mic":   {"mic-on.png", "mic-off.png", "no-meeting-mic.png", "muted"},
	"video": {"video-on.png", "video-off.png", "no-meeting-video.png", "video_off"},
	"hand":  {"hand-down.png", "hand-up.png", "no-meeting-hand.png", "hand_raised"},
	"leave": {"leave.png", "leave.png", "no-meeting-leave.png", ""},
}

type zoomToggleSpec struct {
	mods  string
	key   string
	field string
}

var zoomToggles = map[string]zoomToggleSpec{
	"mute":  {"ALT", "a", "muted"},
	"video": {"ALT", "v", "video_off"},
	"hand":  {"ALT", "y", "hand_raised"},
}

var mainRowSlots = [5]string{"mute", "video", "hand", "share", "leave"}

var mainRowIconButton = map[string]string{
	"mute":  "mic",
	"video": "video",
	"hand":  "hand",
	"leave": "leave",
}

func hyprctl(args ...string) (string, error) {
	ensureHyprlandEnv()
	cmd := exec.Command("hyprctl", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hyprctlClients() ([]hyprClient, error) {
	out, err := hyprctl("clients", "-j")
	if err != nil || out == "" {
		return nil, err
	}
	var clients []hyprClient
	if err := json.Unmarshal([]byte(out), &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

func activeWindowAddress() string {
	out, err := hyprctl("activewindow", "-j")
	if err != nil || out == "" {
		return ""
	}
	var obj struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return ""
	}
	return obj.Address
}

func findMeetingWindowFromClients(clients []hyprClient) string {
	var candidates []hyprClient
	for _, c := range clients {
		if strings.ToLower(c.Class) != "zoom" {
			continue
		}
		if !c.Mapped {
			continue
		}
		title := strings.ToLower(c.Title)
		skip := false
		for _, m := range nonMeetingTitleMarkers {
			if strings.Contains(title, m) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		candidates = append(candidates, c)
	}
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c.Title), "meeting") {
			return c.Address
		}
	}
	if len(candidates) > 0 {
		return candidates[0].Address
	}
	return ""
}

func isSharingFromClients(clients []hyprClient) bool {
	for _, c := range clients {
		if strings.ToLower(c.Class) != "zoom" {
			continue
		}
		if strings.Contains(strings.ToLower(c.Title), "as_toolbar") {
			return true
		}
	}
	return false
}

func findMeetingWindow() string {
	clients, err := hyprctlClients()
	if err != nil {
		return ""
	}
	return findMeetingWindowFromClients(clients)
}

func isSharing() bool {
	clients, err := hyprctlClients()
	if err != nil {
		return false
	}
	return isSharingFromClients(clients)
}

func waitForMeetingWindow(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		clients, err := hyprctlClients()
		if err == nil {
			for _, c := range clients {
				if strings.ToLower(c.Class) != "zoom" || !c.Mapped {
					continue
				}
				title := strings.ToLower(c.Title)
				skip := strings.Contains(title, "as_toolbar")
				if !skip {
					for _, m := range nonMeetingTitleMarkers {
						if strings.Contains(title, m) {
							skip = true
							break
						}
					}
				}
				if !skip {
					return c.Address
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return findMeetingWindow()
}

func hyprFocus(address string) {
	if address != "" {
		_, _ = hyprctl("dispatch", fmt.Sprintf(`hl.dsp.focus({window="address:%s"})`, address))
	}
}

func hyprSendShortcut(mods, key, address string) {
	_, _ = hyprctl("dispatch", fmt.Sprintf(`hl.dsp.send_shortcut({mods="%s", key="%s", window="address:%s"})`, mods, key, address))
}

func sendZoomShortcut(mods, key, meeting string) {
	if meeting == "" {
		meeting = findMeetingWindow()
	}
	if meeting == "" {
		return
	}
	prev := activeWindowAddress()
	if prev != "" && strings.EqualFold(prev, meeting) {
		hyprSendShortcut(mods, key, meeting)
		return
	}
	hyprFocus(meeting)
	time.Sleep(focusSettle)
	hyprSendShortcut(mods, key, meeting)
	time.Sleep(focusSettle)
	hyprFocus(prev)
}

var kvRe = regexp.MustCompile(`(\w+)=(\S+)`)

func loadZoomState(meeting string) map[string]string {
	return loadZoomStateFromFile(zoomStateFile, meeting)
}

func loadZoomStateFromFile(path, meeting string) map[string]string {
	state := map[string]string{
		"muted":       "0",
		"video_off":   "0",
		"hand_raised": "0",
	}
	raw := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		for _, m := range kvRe.FindAllStringSubmatch(string(data), -1) {
			raw[m[1]] = m[2]
		}
	}
	for k := range state {
		if v, ok := raw[k]; ok {
			state[k] = v
		}
	}
	if meeting != "" && raw["meeting"] != "" && raw["meeting"] != meeting {
		state["muted"] = "0"
		state["video_off"] = "0"
		state["hand_raised"] = "0"
	}
	if meeting != "" {
		state["meeting"] = meeting
	}
	return state
}

func saveZoomState(state map[string]string) {
	saveZoomStateToFile(zoomStateFile, state)
}

func saveZoomStateToFile(path string, state map[string]string) {
	keys := []string{"muted", "video_off", "hand_raised", "meeting"}
	var parts []string
	for _, k := range keys {
		if v, ok := state[k]; ok && v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	_ = atomicWriteFile(path, []byte(strings.Join(parts, " ")+"\n"), 0o644)
}

func clearZoomState() {
	_ = os.Remove(zoomStateFile)
}

func zoomIconFor(button, meeting string) string {
	return zoomIconForStateFile(zoomStateFile, button, meeting)
}

func zoomIconForStateFile(statePath, button, meeting string) string {
	spec, ok := zoomIcons[button]
	if !ok {
		return ""
	}
	if meeting == "" {
		return assetPath(filepath.Join("zoom", "assets", spec.noMeeting))
	}
	if spec.field == "" {
		return assetPath(filepath.Join("zoom", "assets", spec.on))
	}
	state := loadZoomStateFromFile(statePath, meeting)
	if state[spec.field] == "1" {
		return assetPath(filepath.Join("zoom", "assets", spec.off))
	}
	return assetPath(filepath.Join("zoom", "assets", spec.on))
}

func mainRowSlotSource(slot int, meeting string, sharing bool) string {
	return mainRowSlotSourceWithDirs(vdesktopOutDir, zoomStateFile, slot, meeting, sharing)
}

func mainRowSlotSourceWithDirs(outDir, statePath string, slot int, meeting string, sharing bool) string {
	action := mainRowSlots[slot]
	if meeting == "" {
		return filepath.Join(outDir, fmt.Sprintf("desk-%d.png", slot))
	}
	if action == "share" {
		if sharing {
			return assetPath(filepath.Join("zoom", "assets", "share-stop.png"))
		}
		return assetPath(filepath.Join("zoom", "assets", "share-start.png"))
	}
	return zoomIconForStateFile(statePath, mainRowIconButton[action], meeting)
}

// updateMainRowSlotsFromClients updates /tmp/streamdeck-vdesktop/slot-0..4.png
// using already-fetched Hyprland clients.
func updateMainRowSlotsFromClients(clients []hyprClient, prevSources *[5]string) {
	updateMainRowSlotsToDir(vdesktopOutDir, zoomStateFile, clients, prevSources)
}

func updateMainRowSlotsToDir(outDir, statePath string, clients []hyprClient, prevSources *[5]string) {
	meeting := findMeetingWindowFromClients(clients)
	if meeting == "" {
		_ = os.Remove(statePath)
	}
	sharing := isSharingFromClients(clients)

	for slot := 0; slot < 5; slot++ {
		src := mainRowSlotSourceWithDirs(outDir, statePath, slot, meeting, sharing)
		dst := filepath.Join(outDir, fmt.Sprintf("slot-%d.png", slot))
		if !fileExists(src) {
			continue
		}
		needCopy := prevSources == nil || !fileExists(dst)
		if !needCopy && prevSources[slot] != src {
			needCopy = true
		}
		if !needCopy && meeting == "" {
			if sSt, err1 := os.Stat(src); err1 == nil {
				if dSt, err2 := os.Stat(dst); err2 == nil {
					if sSt.ModTime().After(dSt.ModTime()) {
						needCopy = true
					}
				}
			}
		}
		if needCopy {
			if err := atomicCopyFile(src, dst); err == nil && prevSources != nil {
				prevSources[slot] = src
			}
		}
	}
}

func refreshMainRowSlots() {
	clients, err := hyprctlClients()
	if err != nil {
		clients = nil
	}
	updateMainRowSlotsFromClients(clients, nil)
}

// RunZoom is the entrypoint for `./deckctl zoom <icon|toggle|share|leave> ...`.
func RunZoom(args []string) int {
	if len(args) < 1 {
		printfErr("usage: zoom icon mic|video|hand|leave | toggle mute|video|hand | share | leave\n")
		return 1
	}
	cmd := args[0]
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}

	switch cmd {
	case "icon":
		if _, ok := zoomIcons[arg]; !ok {
			printfErr("usage: zoom icon mic|video|hand|leave\n")
			return 1
		}
		meeting := findMeetingWindow()
		if meeting == "" {
			clearZoomState()
		}
		fmt.Println(zoomIconFor(arg, meeting))
		return 0

	case "toggle":
		spec, ok := zoomToggles[arg]
		if !ok {
			printfErr("usage: zoom toggle mute|video|hand\n")
			return 1
		}
		meeting := findMeetingWindow()
		if meeting == "" {
			return 0
		}
		sendZoomShortcut(spec.mods, spec.key, meeting)
		state := loadZoomState(meeting)
		if state[spec.field] == "1" {
			state[spec.field] = "0"
		} else {
			state[spec.field] = "1"
		}
		saveZoomState(state)
		refreshMainRowSlots()
		return 0

	case "share":
		meeting := findMeetingWindow()
		if meeting != "" {
			sendZoomShortcut("ALT", "s", meeting)
			refreshMainRowSlots()
		}
		return 0

	case "leave":
		meeting := findMeetingWindow()
		if meeting == "" {
			return 0
		}
		if isSharing() {
			sendZoomShortcut("ALT", "s", meeting)
			meeting = waitForMeetingWindow(3 * time.Second)
		}
		if meeting != "" {
			sendZoomShortcut("ALT", "q", meeting)
		}
		clearZoomState()
		refreshMainRowSlots()
		return 0

	default:
		printfErr("usage: zoom icon mic|video|hand|leave | toggle mute|video|hand | share | leave\n")
		return 1
	}
}

// RunMainRow is the entrypoint for `./deckctl mainrow <icon|press> <slot 0-4>`.
func RunMainRow(args []string) int {
	if len(args) < 2 {
		printfErr("usage: mainrow icon|press <slot 0-4>\n")
		return 1
	}
	cmd := args[0]
	slot, err := strconv.Atoi(args[1])
	if err != nil || slot < 0 || slot >= len(mainRowSlots) {
		printfErr("usage: mainrow icon|press <slot 0-4>\n")
		return 1
	}

	switch cmd {
	case "icon":
		clients, _ := hyprctlClients()
		meeting := findMeetingWindowFromClients(clients)
		if meeting == "" {
			clearZoomState()
		}
		sharing := isSharingFromClients(clients)
		fmt.Println(mainRowSlotSource(slot, meeting, sharing))
		return 0

	case "press":
		action := mainRowSlots[slot]
		meeting := findMeetingWindow()
		if meeting == "" {
			rc := RunVDesktop([]string{"switch", strconv.Itoa(slot)})
			refreshMainRowSlots()
			return rc
		}
		switch action {
		case "leave":
			return RunZoom([]string{"leave"})
		case "share":
			return RunZoom([]string{"share"})
		default:
			return RunZoom([]string{"toggle", action})
		}

	default:
		printfErr("usage: mainrow icon|press <slot 0-4>\n")
		return 1
	}
}
