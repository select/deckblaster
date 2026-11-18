package sysplugins

import (
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestHACardRendering(t *testing.T) {
	// Door open vs closed
	openImg := renderDoorCard(true, "OPEN", "Haustür")
	closedImg := renderDoorCard(false, "4h", "Haustür")
	if openImg.Bounds().Dx() != 72 || openImg.Bounds().Dy() != 72 {
		t.Fatalf("door open card size = %v, want 72x72", openImg.Bounds())
	}
	if closedImg.Bounds().Dx() != 72 || closedImg.Bounds().Dy() != 72 {
		t.Fatalf("door closed card size = %v, want 72x72", closedImg.Bounds())
	}
	if string(openImg.Pix) == string(closedImg.Pix) {
		t.Fatal("open and closed door cards should differ")
	}

	// Moisture dry / ok / wet
	dry := renderMoistureCard(15, "Pflanze")
	ok := renderMoistureCard(45, "Pflanze")
	wet := renderMoistureCard(85, "Pflanze")
	for name, img := range map[string]*image.RGBA{"dry": dry, "ok": ok, "wet": wet} {
		if img.Bounds().Dx() != 72 || img.Bounds().Dy() != 72 {
			t.Fatalf("moisture %s card size = %v, want 72x72", name, img.Bounds())
		}
	}
	if string(dry.Pix) == string(ok.Pix) || string(ok.Pix) == string(wet.Pix) {
		t.Fatal("moisture tiers should render distinct cards")
	}

	// Bell ringing vs idle
	ring := renderBellCard(true, "NOW!", "Klingel")
	idle := renderBellCard(false, "2h", "Klingel")
	if ring.Bounds().Dx() != 72 || ring.Bounds().Dy() != 72 {
		t.Fatalf("bell card size = %v, want 72x72", ring.Bounds())
	}
	if string(ring.Pix) == string(idle.Pix) {
		t.Fatal("ringing and idle bell cards should differ")
	}

	// Mower badge healthy vs error
	base := image.NewRGBA(image.Rect(0, 0, 72, 72))
	fillRectSrc(base, 0, 0, 72, 72, color.RGBA{20, 20, 30, 255})
	healthyImg := renderMowerBadge(base, true)
	errImg := renderMowerBadge(base, false)
	if string(healthyImg.Pix) != string(base.Pix) {
		t.Fatal("healthy mower badge should match base image")
	}
	if string(errImg.Pix) == string(base.Pix) {
		t.Fatal("error mower badge should draw red badge on base image")
	}
	// Bottom-right area should contain red badge pixels
	c := errImg.RGBAAt(54, 54)
	if c.R < 150 {
		t.Fatalf("expected red badge pixel at (54,54), got %+v", c)
	}
}

func TestHABatchedStateCache(t *testing.T) {
	var batchCalls int32
	var singleCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/states":
			atomic.AddInt32(&batchCalls, 1)
			_ = json.NewEncoder(w).Encode([]haEntityState{
				{EntityID: "light.esstisch", State: "on", LastChanged: "2026-01-01T12:00:00Z"},
				{EntityID: "binary_sensor.haustur_contact", State: "off", LastChanged: "2026-01-01T11:00:00Z"},
			})
		case "/api/states/sensor.missing_from_batch":
			atomic.AddInt32(&singleCalls, 1)
			_ = json.NewEncoder(w).Encode(haEntityState{
				EntityID: "sensor.missing_from_batch",
				State:    "42",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	c := &haClient{
		baseURL: srv.URL,
		token:   "test-token",
		http:    srv.Client(),
		cache:   tmpDir,
	}

	// Multiple entity lookups should result in only 1 /api/states call
	s1, _ := c.getEntityStateAndTime("light.esstisch")
	s2, _ := c.getEntityStateAndTime("binary_sensor.haustur_contact")
	if s1 != "on" || s2 != "off" {
		t.Fatalf("unexpected states: s1=%q s2=%q", s1, s2)
	}
	if got := atomic.LoadInt32(&batchCalls); got != 1 {
		t.Fatalf("batchCalls = %d, want 1", got)
	}

	// Invalidate cache and verify next lookup refreshes
	c.invalidateStatesCache()
	s1, _ = c.getEntityStateAndTime("light.esstisch")
	if s1 != "on" {
		t.Fatalf("unexpected state after invalidate: %q", s1)
	}
	if got := atomic.LoadInt32(&batchCalls); got != 2 {
		t.Fatalf("batchCalls after invalidate = %d, want 2", got)
	}

	// Fallback to single entity endpoint if not in batch map
	s3 := c.getState("sensor.missing_from_batch")
	if s3 != "42" {
		t.Fatalf("fallback state = %q, want 42", s3)
	}
	if got := atomic.LoadInt32(&singleCalls); got != 1 {
		t.Fatalf("singleCalls = %d, want 1", got)
	}
}

func TestVDesktopRenderingAndHyprClients(t *testing.T) {
	emptyInactive := renderDesktopImage(nil, false)
	emptyActive := renderDesktopImage(nil, true)

	if emptyInactive.RGBAAt(10, 10) != (color.RGBA{0x3a, 0x3a, 0x3a, 255}) {
		t.Fatalf("unexpected empty inactive bg: %+v", emptyInactive.RGBAAt(10, 10))
	}
	if emptyActive.RGBAAt(10, 70) != (color.RGBA{0x18, 0xbc, 0xf2, 255}) {
		t.Fatalf("expected active underline #18bcf2 at y=70, got %+v", emptyActive.RGBAAt(10, 70))
	}
	if emptyInactive.RGBAAt(10, 70) == (color.RGBA{0x18, 0xbc, 0xf2, 255}) {
		t.Fatal("inactive desktop should not have active underline")
	}

	clients := []hyprClient{
		{Address: "0x1", Class: "Kitty", Mapped: true},
		{Address: "0x2", Class: "kitty", Mapped: true}, // duplicate on same ws
		{Address: "0x3", Class: "Firefox", Mapped: false},
	}
	clients[0].Workspace.ID = 1
	clients[1].Workspace.ID = 1
	clients[2].Workspace.ID = 2

	desks := desktopsFromHyprClients(clients)
	if len(desks[0]) != 1 || desks[0][0] != "kitty" {
		t.Fatalf("desk 0 apps = %v, want [kitty]", desks[0])
	}
	if len(desks[1]) != 0 {
		t.Fatalf("unmapped client should not appear on desk 1: %v", desks[1])
	}
}

func TestZoomWindowDetectionAndState(t *testing.T) {
	hub := hyprClient{Address: "0x10", Class: "zoom", Title: "Zoom Workplace", Mapped: true}
	settings := hyprClient{Address: "0x11", Class: "Zoom", Title: "Settings", Mapped: true}
	unmapped := hyprClient{Address: "0x12", Class: "Zoom", Title: "Zoom Meeting", Mapped: false}
	meeting := hyprClient{Address: "0x13", Class: "Zoom", Title: "Zoom Meeting", Mapped: true}
	toolbar := hyprClient{Address: "0x14", Class: "Zoom", Title: "as_toolbar", Mapped: true}

	if got := findMeetingWindowFromClients([]hyprClient{hub, settings, unmapped}); got != "" {
		t.Fatalf("expected no meeting window, got %q", got)
	}
	if got := findMeetingWindowFromClients([]hyprClient{hub, meeting}); got != "0x13" {
		t.Fatalf("expected meeting 0x13, got %q", got)
	}
	if isSharingFromClients([]hyprClient{hub, meeting}) {
		t.Fatal("should not report sharing without as_toolbar")
	}
	if !isSharingFromClients([]hyprClient{hub, toolbar}) {
		t.Fatal("should report sharing when as_toolbar is present")
	}

	// State persistence + reset on meeting change
	statePath := filepath.Join(t.TempDir(), "zoom-state")
	saveZoomStateToFile(statePath, map[string]string{
		"muted":       "1",
		"video_off":   "1",
		"hand_raised": "0",
		"meeting":     "0x13",
	})
	stSame := loadZoomStateFromFile(statePath, "0x13")
	if stSame["muted"] != "1" || stSame["video_off"] != "1" {
		t.Fatalf("expected preserved state for same meeting, got %v", stSame)
	}
	stNew := loadZoomStateFromFile(statePath, "0x99")
	if stNew["muted"] != "0" || stNew["video_off"] != "0" || stNew["meeting"] != "0x99" {
		t.Fatalf("expected reset state for new meeting, got %v", stNew)
	}
}

func TestRecorderIconAndStatus(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "recorder.pid")
	statusFile := filepath.Join(tmp, "status-icon.png")

	// Status icon < 10s old should be returned
	img := renderRecorderStatus("Encoding...", color.RGBA{255, 200, 50, 255}, color.RGBA{30, 25, 0, 255})
	if img.Bounds().Dx() != 72 || img.Bounds().Dy() != 72 {
		t.Fatalf("status icon size = %v, want 72x72", img.Bounds())
	}
	if err := atomicWritePNG(statusFile, img); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if got := recorderIconPath(tmp, pidFile, statusFile, now); got != statusFile {
		t.Fatalf("expected status icon %q, got %q", statusFile, got)
	}

	// Expired status icon (>= 10s) should be removed and fall back to record.png
	oldTime := now.Add(-15 * time.Second)
	_ = os.Chtimes(statusFile, oldTime, oldTime)
	got := recorderIconPath(tmp, pidFile, statusFile, now)
	if filepath.Base(got) != "record.png" {
		t.Fatalf("expected record.png after status expiry, got %q", got)
	}
	if fileExists(statusFile) {
		t.Fatal("expired status icon should have been removed")
	}

	// Live PID (current test process) should return record-stop.png
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	got = recorderIconPath(tmp, pidFile, statusFile, now)
	if filepath.Base(got) != "record-stop.png" {
		t.Fatalf("expected record-stop.png for live PID, got %q", got)
	}
}

func TestMainRowSlotsImmediateUpdateOnZoomToggle(t *testing.T) {
	outDir := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "zoom-state")
	clients := []hyprClient{
		{Address: "0x42", Class: "Zoom", Title: "Zoom Meeting", Mapped: true},
	}

	// Initially unmuted -> slot-0.png should match mic-on.png
	saveZoomStateToFile(statePath, map[string]string{
		"muted":       "0",
		"video_off":   "0",
		"hand_raised": "0",
		"meeting":     "0x42",
	})
	updateMainRowSlotsToDir(outDir, statePath, clients, nil)

	slot0 := filepath.Join(outDir, "slot-0.png")
	micOnBytes, err := os.ReadFile(assetPath(filepath.Join("zoom", "assets", "mic-on.png")))
	if err != nil {
		t.Fatal(err)
	}
	micOffBytes, err := os.ReadFile(assetPath(filepath.Join("zoom", "assets", "mic-off.png")))
	if err != nil {
		t.Fatal(err)
	}
	got1, err := os.ReadFile(slot0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got1) != string(micOnBytes) {
		t.Fatal("expected slot-0.png to match mic-on.png initially")
	}

	// Flip mute state and call with prevSources == nil -> slot-0.png must update immediately
	saveZoomStateToFile(statePath, map[string]string{
		"muted":       "1",
		"video_off":   "0",
		"hand_raised": "0",
		"meeting":     "0x42",
	})
	updateMainRowSlotsToDir(outDir, statePath, clients, nil)

	got2, err := os.ReadFile(slot0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != string(micOffBytes) {
		t.Fatal("expected slot-0.png to update to mic-off.png immediately when prevSources is nil")
	}
}
