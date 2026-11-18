package sysplugins

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nfnt/resize"
)

const (
	recDir        = "/tmp/streamdeck-recording"
	recFramesDir  = "/tmp/streamdeck-recording/frames"
	recCompDir    = "/tmp/streamdeck-recording/composited"
	recStateFile  = "/tmp/streamdeck-recording/state.json"
	recPIDFile    = "/tmp/streamdeck-recording/recorder.pid"
	recStatusIcon = "/tmp/streamdeck-recording/status-icon.png"
)

type recorderState struct {
	FPS        int     `json:"fps"`
	Started    float64 `json:"started"`
	FrameCount int     `json:"frame_count"`
}

func videoOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/tmp"
	}
	return filepath.Join(home, "Videos")
}

func loadRecorderState(path string) recorderState {
	var st recorderState
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func saveRecorderState(path string, st recorderState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, b, 0o644)
}

func isPIDAlive(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return pid, false
	}
	return pid, true
}

func renderRecorderStatus(text string, fg, bg color.RGBA) *image.RGBA {
	const size = 72
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRectSrc(img, 0, 0, size, size, bg)
	drawRoundedRectOutline(img, 3, 3, 69, 69, 8, color.RGBA{60, 60, 80, 255})

	bold, _ := loadDejaVuBold()
	face := newFace(bold, 11)
	if face == nil {
		return img
	}
	defer face.Close()

	var lines []string
	for _, word := range strings.Fields(text) {
		if len(lines) > 0 {
			test := lines[len(lines)-1] + " " + word
			if measureTextWidth(face, test) <= 62 {
				lines[len(lines)-1] = test
				continue
			}
		}
		lines = append(lines, word)
	}

	totalH := len(lines) * 14
	y := (size - totalH) / 2
	for _, line := range lines {
		drawCenteredText(img, face, line, size/2, y, fg)
		y += 14
	}
	return img
}

func writeRecorderStatus(text string, fg, bg color.RGBA) {
	img := renderRecorderStatus(text, fg, bg)
	_ = atomicWritePNG(recStatusIcon, img)
}

func recorderIconPath(dir, pidFile, statusFile string, now time.Time) string {
	if st, err := os.Stat(statusFile); err == nil {
		if now.Sub(st.ModTime()) < 10*time.Second {
			return statusFile
		}
		_ = os.Remove(statusFile)
	}
	if _, alive := isPIDAlive(pidFile); alive {
		return assetPath(filepath.Join("recorder", "assets", "record-stop.png"))
	}
	return assetPath(filepath.Join("recorder", "assets", "record.png"))
}

func spawnDetachedSelf(subArgs ...string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, subArgs...)
	cmd.Dir = resolveBaseDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		defer devNull.Close()
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func cmdRecorderStart(fps int) int {
	if fps <= 0 {
		fps = 5
	}
	_ = os.MkdirAll(recFramesDir, 0o755)
	_ = os.MkdirAll(videoOutputDir(), 0o755)
	_ = os.Remove(recStatusIcon)

	if entries, err := os.ReadDir(recFramesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				_ = os.Remove(filepath.Join(recFramesDir, e.Name()))
			}
		}
	}

	st := recorderState{
		FPS:        fps,
		Started:    float64(time.Now().UnixNano()) / 1e9,
		FrameCount: 0,
	}
	_ = saveRecorderState(recStateFile, st)

	pid, err := spawnDetachedSelf("recorder", "_daemon", "--fps", strconv.Itoa(fps))
	if err != nil {
		printfErr("recorder start: %v\n", err)
		return 1
	}
	if err := atomicWriteFile(recPIDFile, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		printfErr("recorder pid write: %v\n", err)
		return 1
	}
	fmt.Printf("Recording started (PID %d, %d fps)\nFrames: %s\n", pid, fps, recFramesDir)
	return 0
}

func cmdRecorderStop() int {
	if pid, alive := isPIDAlive(recPIDFile); alive {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	_ = os.Remove(recPIDFile)

	writeRecorderStatus("Encoding...", color.RGBA{255, 200, 50, 255}, color.RGBA{30, 25, 0, 255})

	if _, err := spawnDetachedSelf("recorder", "_encode"); err != nil {
		printfErr("recorder spawn encode: %v\n", err)
		return 1
	}
	return 0
}

func cmdRecorderStatus() int {
	if _, err := os.Stat(recPIDFile); err != nil {
		fmt.Println("Not recording.")
		return 0
	}
	if _, alive := isPIDAlive(recPIDFile); !alive {
		fmt.Println("Not recording (stale PID).")
		return 0
	}
	st := loadRecorderState(recStateFile)
	elapsed := time.Since(time.Unix(int64(st.Started), 0)).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	fmt.Printf("Recording: %d frames, %.0fs elapsed\n", st.FrameCount, elapsed)
	return 0
}

func cmdRecorderDaemon(fps int) int {
	if fps <= 0 {
		fps = 5
	}
	_ = os.MkdirAll(recFramesDir, 0o755)
	apiURL := strings.TrimRight(getEnvOrConfig("DECK_API", "http://localhost:9990"), "/") + "/screenshot"

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	client := &http.Client{Timeout: 2 * time.Second}
	interval := time.Second / time.Duration(fps)
	st := loadRecorderState(recStateFile)
	if st.Started == 0 {
		st.Started = float64(time.Now().UnixNano()) / 1e9
	}
	st.FPS = fps
	frame := 0

	for {
		select {
		case <-sigCh:
			return 0
		default:
		}

		t0 := time.Now()
		resp, err := client.Get(apiURL)
		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if data, err := io.ReadAll(resp.Body); err == nil && len(data) > 0 {
					framePath := filepath.Join(recFramesDir, fmt.Sprintf("frame-%06d.png", frame))
					if atomicWriteFile(framePath, data, 0o644) == nil {
						frame++
						st.FrameCount = frame
						_ = saveRecorderState(recStateFile, st)
					}
				}
			}
			_ = resp.Body.Close()
		}

		elapsed := time.Since(t0)
		if elapsed < interval {
			select {
			case <-sigCh:
				return 0
			case <-time.After(interval - elapsed):
			}
		}
	}
}

func cmdRecorderEncode() int {
	st := loadRecorderState(recStateFile)
	fps := st.FPS
	if fps <= 0 {
		fps = 5
	}
	if st.FrameCount == 0 {
		writeRecorderStatus("No frames", color.RGBA{255, 80, 80, 255}, color.RGBA{0, 0, 0, 255})
		return 0
	}

	writeRecorderStatus("Compositing...", color.RGBA{255, 200, 50, 255}, color.RGBA{30, 25, 0, 255})

	templatePath := assetPath(filepath.Join("recorder", "assets", "steamdeck-template.png"))
	useTemplate := fileExists(templatePath)

	inputPattern := filepath.Join(recFramesDir, "frame-%06d.png")
	vf := "scale=iw*3:ih*3:flags=neighbor"

	if useTemplate {
		if f, err := os.Open(templatePath); err == nil {
			tmplImg, _, decErr := image.Decode(f)
			_ = f.Close()
			if decErr == nil {
				tb := tmplImg.Bounds()
				tmpl2x := resize.Resize(uint(tb.Dx()*2), uint(tb.Dy()*2), tmplImg, resize.Lanczos3)
				_ = os.MkdirAll(recCompDir, 0o755)

				const (
					keySrc    = 72
					gapSrc    = 4
					keyDst    = 84
					vGap      = 9
					x0        = 358
					y0        = 309
					gridRight = 812
					cols      = 5
					rows      = 3
				)
				var colXs [cols]int
				for c := 0; c < cols; c++ {
					colXs[c] = x0 + (c*(gridRight-x0-keyDst))/4
				}

				for i := 0; i < st.FrameCount; i++ {
					srcPath := filepath.Join(recFramesDir, fmt.Sprintf("frame-%06d.png", i))
					sf, err := os.Open(srcPath)
					if err != nil {
						continue
					}
					srcImg, _, err := image.Decode(sf)
					_ = sf.Close()
					if err != nil {
						continue
					}

					comp := image.NewRGBA(tmpl2x.Bounds())
					draw.Draw(comp, comp.Bounds(), tmpl2x, tmpl2x.Bounds().Min, draw.Src)

					for r := 0; r < rows; r++ {
						for c := 0; c < cols; c++ {
							sx := c * (keySrc + gapSrc)
							sy := r * (keySrc + gapSrc)
							cropRect := image.Rect(sx, sy, sx+keySrc, sy+keySrc)
							keySub := image.NewRGBA(image.Rect(0, 0, keySrc, keySrc))
							draw.Draw(keySub, keySub.Bounds(), srcImg, cropRect.Min, draw.Src)
							keyResized := resize.Resize(keyDst, keyDst, keySub, resize.Lanczos3)
							dx := colXs[c]
							dy := y0 + r*(keyDst+vGap)
							draw.Draw(comp, image.Rect(dx, dy, dx+keyDst, dy+keyDst), keyResized, image.Point{}, draw.Over)
						}
					}
					compPath := filepath.Join(recCompDir, fmt.Sprintf("frame-%06d.png", i))
					_ = atomicWritePNG(compPath, comp)
				}
				inputPattern = filepath.Join(recCompDir, "frame-%06d.png")
				vf = "scale=iw*2:ih*2:flags=lanczos,pad=ceil(iw/2)*2:ceil(ih/2)*2"
			}
		}
	}

	writeRecorderStatus("Encoding...", color.RGBA{255, 200, 50, 255}, color.RGBA{30, 25, 0, 255})

	_ = os.MkdirAll(videoOutputDir(), 0o755)
	ts := time.Now().Format("20060102-150405")
	output := filepath.Join(videoOutputDir(), fmt.Sprintf("streamdeck-%s.mp4", ts))

	cmd := exec.Command("ffmpeg", "-y",
		"-framerate", strconv.Itoa(fps),
		"-i", inputPattern,
		"-vf", vf,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-crf", "18",
		output,
	)
	if err := cmd.Run(); err != nil {
		writeRecorderStatus("Encode FAIL", color.RGBA{255, 60, 60, 255}, color.RGBA{0, 0, 0, 255})
		return 1
	}

	writeRecorderStatus("Saved! "+ts, color.RGBA{80, 255, 120, 255}, color.RGBA{10, 30, 10, 255})
	return 0
}

// RunRecorder is the entrypoint for `./deckctl recorder <icon|toggle|start|stop|status|_daemon|_encode>`.
func RunRecorder(args []string) int {
	if len(args) < 1 {
		printfErr("usage: recorder <icon|toggle|start [--fps N]|stop|status>\n")
		return 1
	}
	cmd := args[0]
	rest := args[1:]

	parseFPS := func(def int) int {
		fps := def
		for i := 0; i < len(rest)-1; i++ {
			if rest[i] == "--fps" {
				if v, err := strconv.Atoi(rest[i+1]); err == nil && v > 0 {
					fps = v
				}
			}
		}
		return fps
	}

	switch cmd {
	case "icon":
		fmt.Println(recorderIconPath(recDir, recPIDFile, recStatusIcon, time.Now()))
		return 0
	case "toggle":
		if _, alive := isPIDAlive(recPIDFile); alive {
			return cmdRecorderStop()
		}
		return cmdRecorderStart(10)
	case "start":
		return cmdRecorderStart(parseFPS(5))
	case "stop":
		return cmdRecorderStop()
	case "status":
		return cmdRecorderStatus()
	case "_daemon":
		return cmdRecorderDaemon(parseFPS(5))
	case "_encode":
		return cmdRecorderEncode()
	default:
		printfErr("unknown recorder subcommand: %s\n", cmd)
		return 1
	}
}
