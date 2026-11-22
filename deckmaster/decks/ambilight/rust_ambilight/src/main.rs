use std::collections::HashMap;
use std::ffi::c_void;
use std::fs::{File, OpenOptions};
use std::io::{BufRead, BufReader};
use std::net::UdpSocket;
use std::os::fd::{AsFd, AsRawFd};
use std::os::unix::fs::MetadataExt;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use gbm::BufferObjectFlags;
use r_egl_wayland::{WayEglTrait, EGL_INSTALCE, r_egl as egl};
use wayland_client::globals::{GlobalListContents, registry_queue_init};
use wayland_client::protocol::wl_buffer::WlBuffer;
use wayland_client::protocol::wl_output::{self, WlOutput};
use wayland_client::protocol::wl_registry::{self, WlRegistry};
use wayland_client::{Connection, Dispatch, EventQueue, Proxy, QueueHandle, WEnum, delegate_noop};
use wayland_protocols::ext::image_capture_source::v1::client::{
    ext_image_capture_source_v1::ExtImageCaptureSourceV1,
    ext_output_image_capture_source_manager_v1::ExtOutputImageCaptureSourceManagerV1,
};
use wayland_protocols::ext::image_copy_capture::v1::client::{
    ext_image_copy_capture_frame_v1::{self, ExtImageCopyCaptureFrameV1, FailureReason},
    ext_image_copy_capture_manager_v1::{ExtImageCopyCaptureManagerV1, Options as CaptureOptions},
    ext_image_copy_capture_session_v1::{self, ExtImageCopyCaptureSessionV1},
};
use wayland_protocols::wp::linux_dmabuf::zv1::client::{
    zwp_linux_buffer_params_v1::{self, ZwpLinuxBufferParamsV1},
    zwp_linux_dmabuf_v1::ZwpLinuxDmabufV1,
};

/// Configuration structure holding all parsed parameters for the Ambilight daemon.
#[derive(Debug)]
struct AppConfig {
    /// The network IP address of the WLED (Wireless Light Emitting Diode) strip.
    wled_ip: String,
    /// The physical number of Light Emitting Diodes (LEDs) on your desk light strip.
    led_count: usize,
    /// Saturation boost factor (e.g., 1.4 raises color intensity by 40%).
    boost: f32,
    /// Loop delay interval in milliseconds (e.g., 33ms target matches 30 Frames Per Second).
    interval_ms: u64,
    /// Layout mapping style: sequential clockwise "perimeter" or dual symmetrical "split-up".
    mapping: String,
    /// True if the direction of the light strip's mapping should be completely reversed.
    reverse: bool,
    /// Shift index to align the starting LED of the physical strip with the screen corner.
    led_offset: i32,
    /// True if the first half segment (Left) in split-up mapping should be reversed.
    reverse_first_half: bool,
    /// True if the second half segment (Right) in split-up mapping should be reversed.
    reverse_second_half: bool,
    /// Optional override name of a specific screen monitor to capture (e.g., "DP-2").
    monitor: Option<String>,
    /// Minimum threshold for any individual RGB color channel change to trigger a network UDP packet.
    threshold: i32,
}

/// JSON representation of a single monitor queried from the Hyprland compositor.
#[derive(serde::Deserialize, Debug)]
struct HyprlandMonitor {
    name: String,
    focused: bool,
}

/// Lightweight cache to store the active monitor name for up to 2 seconds.
struct MonitorCache {
    name: Option<String>,
    last_check: Option<Instant>,
}

impl MonitorCache {
    fn new() -> Self {
        Self {
            name: None,
            last_check: None,
        }
    }

    fn get_focused(&mut self) -> Option<String> {
        let now = Instant::now();
        if let Some(last) = self.last_check {
            if now.duration_since(last) < Duration::from_secs(2) {
                return self.name.clone();
            }
        }

        self.last_check = Some(now);
        self.name = get_focused_monitor();
        self.name.clone()
    }
}

/// Shells out to `hyprctl monitors -j` to identify which screen is focused.
fn get_focused_monitor() -> Option<String> {
    let output = std::process::Command::new("hyprctl")
        .args(["monitors", "-j"])
        .output()
        .ok()?;

    if output.status.success() {
        let monitors: Vec<HyprlandMonitor> = serde_json::from_slice(&output.stdout).ok()?;
        for m in monitors {
            if m.focused {
                return Some(m.name);
            }
        }
    }
    None
}

/// Reads a standard environment config file line-by-line, parsing "KEY=VALUE" pairs.
fn load_env_file<P: AsRef<Path>>(path: P, env_map: &mut HashMap<String, String>) {
    if let Ok(file) = File::open(path) {
        let reader = BufReader::new(file);
        for line in reader.lines().map_while(Result::ok) {
            let trimmed = line.trim();
            if trimmed.is_empty() || trimmed.starts_with('#') {
                continue;
            }
            if let Some(pos) = trimmed.find('=') {
                let k = trimmed[..pos].trim().to_string();
                let mut v = trimmed[pos + 1..].trim().to_string();
                if ((v.starts_with('"') && v.ends_with('"'))
                    || (v.starts_with('\'') && v.ends_with('\'')))
                    && v.len() >= 2
                {
                    v = v[1..v.len() - 1].to_string();
                }
                env_map.insert(k, v);
            }
        }
    }
}

/// Searches for and merges configurations from `deckblaster.env` and `streamdeck.env` in `~/.config/`.
fn get_env_map() -> HashMap<String, String> {
    let mut env_map = HashMap::new();
    if let Ok(home) = std::env::var("HOME") {
        let deckblaster_env = format!("{}/.config/deckblaster.env", home);
        load_env_file(&deckblaster_env, &mut env_map);
        let streamdeck_env = format!("{}/.config/streamdeck.env", home);
        load_env_file(&streamdeck_env, &mut env_map);
    }
    env_map
}

/// Parses CLI arguments and merges them with environment file configurations.
fn parse_args(env: &HashMap<String, String>) -> AppConfig {
    let mut wled_ip = env.get("WLED_SCHREIBTISCH_IP").cloned().unwrap_or_default();
    let mut led_count = env
        .get("AMBILIGHT_LED_COUNT")
        .and_then(|v| v.parse().ok())
        .unwrap_or(80);
    let mut boost = 1.4f32;
    let mut interval_sec = 0.033f32; // Default to 30 FPS to cap worst-case 4K capture load
    let mut mapping = env
        .get("AMBILIGHT_MAPPING")
        .cloned()
        .unwrap_or_else(|| "perimeter".to_string());
    let mut reverse = env
        .get("AMBILIGHT_REVERSE")
        .map(|v| v.to_lowercase() == "true")
        .unwrap_or(false);
    let mut led_offset = env
        .get("AMBILIGHT_LED_OFFSET")
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    let mut reverse_first_half = env
        .get("AMBILIGHT_REVERSE_FIRST_HALF")
        .map(|v| v.to_lowercase() == "true")
        .unwrap_or(false);
    let mut reverse_second_half = env
        .get("AMBILIGHT_REVERSE_SECOND_HALF")
        .map(|v| v.to_lowercase() == "true")
        .unwrap_or(false);
    let mut monitor = None;
    let mut threshold = 1;

    let args: Vec<String> = std::env::args().collect();
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--wled-ip" => {
                if i + 1 < args.len() {
                    wled_ip = args[i + 1].clone();
                    i += 2;
                } else {
                    eprintln!("Error: --wled-ip requires a value");
                    std::process::exit(1);
                }
            }
            "--led-count" => {
                if i + 1 < args.len() {
                    led_count = args[i + 1].parse().unwrap_or(80);
                    i += 2;
                } else {
                    eprintln!("Error: --led-count requires a value");
                    std::process::exit(1);
                }
            }
            "--boost" => {
                if i + 1 < args.len() {
                    boost = args[i + 1].parse().unwrap_or(1.4);
                    i += 2;
                } else {
                    eprintln!("Error: --boost requires a value");
                    std::process::exit(1);
                }
            }
            "--interval" => {
                if i + 1 < args.len() {
                    interval_sec = args[i + 1].parse().unwrap_or(0.033);
                    i += 2;
                } else {
                    eprintln!("Error: --interval requires a value");
                    std::process::exit(1);
                }
            }
            "--mapping" => {
                if i + 1 < args.len() {
                    mapping = args[i + 1].clone();
                    i += 2;
                } else {
                    eprintln!("Error: --mapping requires a value");
                    std::process::exit(1);
                }
            }
            "--reverse" => {
                reverse = true;
                i += 1;
            }
            "--led-offset" => {
                if i + 1 < args.len() {
                    led_offset = args[i + 1].parse().unwrap_or(0);
                    i += 2;
                } else {
                    eprintln!("Error: --led-offset requires a value");
                    std::process::exit(1);
                }
            }
            "--reverse-first-half" => {
                reverse_first_half = true;
                i += 1;
            }
            "--reverse-second-half" => {
                reverse_second_half = true;
                i += 1;
            }
            "--monitor" => {
                if i + 1 < args.len() {
                    monitor = Some(args[i + 1].clone());
                    i += 2;
                } else {
                    eprintln!("Error: --monitor requires a value");
                    std::process::exit(1);
                }
            }
            "--threshold" => {
                if i + 1 < args.len() {
                    threshold = args[i + 1].parse().unwrap_or(1);
                    i += 2;
                } else {
                    eprintln!("Error: --threshold requires a value");
                    std::process::exit(1);
                }
            }
            _ => {
                eprintln!("Unknown argument: {}", args[i]);
                std::process::exit(1);
            }
        }
    }

    AppConfig {
        wled_ip,
        led_count,
        boost,
        interval_ms: (interval_sec * 1000.0) as u64,
        mapping,
        reverse,
        led_offset,
        reverse_first_half,
        reverse_second_half,
        monitor,
        threshold,
    }
}

/// Converts RGB (0.0..=1.0) into HSV (0.0..=1.0).
fn rgb_to_hsv(r: f32, g: f32, b: f32) -> (f32, f32, f32) {
    let min = r.min(g).min(b);
    let max = r.max(g).max(b);
    let delta = max - min;

    let v = max;
    let s = if max == 0.0 { 0.0 } else { delta / max };

    let mut h = 0.0;
    if delta > 0.0 {
        if max == r {
            h = (g - b) / delta + (if g < b { 6.0 } else { 0.0 });
        } else if max == g {
            h = (b - r) / delta + 2.0;
        } else if max == b {
            h = (r - g) / delta + 4.0;
        }
        h /= 6.0;
    }

    (h, s, v)
}

/// Converts HSV (0.0..=1.0) back into RGB (0.0..=1.0).
fn hsv_to_rgb(h: f32, s: f32, v: f32) -> (f32, f32, f32) {
    let i = (h * 6.0).floor() as i32;
    let f = h * 6.0 - i as f32;
    let p = v * (1.0 - s);
    let q = v * (1.0 - f * s);
    let t = v * (1.0 - (1.0 - f) * s);

    let (r, g, b) = match i.rem_euclid(6) {
        0 => (v, t, p),
        1 => (q, v, p),
        2 => (p, v, t),
        3 => (p, q, v),
        4 => (t, p, v),
        _ => (v, p, q),
    };

    (r, g, b)
}

/// Boosts the saturation of an RGB pixel.
fn boost_saturation(r: u8, g: u8, b: u8, boost: f32) -> (u8, u8, u8) {
    if boost == 1.0 {
        return (r, g, b);
    }
    let (h, s, v) = rgb_to_hsv(r as f32 / 255.0, g as f32 / 255.0, b as f32 / 255.0);
    let s_boosted = (s * boost).min(1.0);
    let (r_b, g_b, b_b) = hsv_to_rgb(h, s_boosted, v);
    (
        (r_b * 255.0).round().clamp(0.0, 255.0) as u8,
        (g_b * 255.0).round().clamp(0.0, 255.0) as u8,
        (b_b * 255.0).round().clamp(0.0, 255.0) as u8,
    )
}

/// Reads a pixel from our 100x100 OpenGL RGBA buffer, flipping Y from top-left to GL bottom-left.
fn get_pixel(pixels: &[u8], x: usize, y_pil: usize) -> (u8, u8, u8) {
    let y_gl = 99 - y_pil;
    let idx = (y_gl * 100 + x) * 4;
    (pixels[idx], pixels[idx + 1], pixels[idx + 2])
}

/// Traces the 100x100 downscaled frame borders and extracts RGB values for each physical LED.
#[allow(clippy::too_many_arguments)]
fn extract_individual_led_colors(
    pixels: &[u8],
    led_count: usize,
    boost: f32,
    reverse: bool,
    led_offset: i32,
    mapping: &str,
    reverse_first_half: bool,
    reverse_second_half: bool,
) -> Vec<(u8, u8, u8)> {
    let mut led_colors = vec![(0, 0, 0); led_count];
    let w = 100;
    let h = 100;

    if mapping == "split-up" {
        let half = led_count / 2;
        let divisor = if half > 1 { half - 1 } else { 1 } as f32;
        let path_len = 150.0f32;

        for i in 0..led_count {
            let mut x;
            let mut y_pil;

            if i < half {
                let d = i as f32 * (path_len / divisor);
                if d < h as f32 {
                    x = 0;
                    y_pil = h - 1 - (d.round() as usize);
                } else {
                    x = (d - h as f32).round() as usize;
                    y_pil = 0;
                }
            } else {
                let j = i - half;
                let d = j as f32 * (path_len / divisor);
                if d < h as f32 {
                    x = w - 1;
                    y_pil = h - 1 - (d.round() as usize);
                } else {
                    x = w - 1 - ((d - h as f32).round() as usize);
                    y_pil = 0;
                }
            }

            x = x.clamp(0, w - 1);
            y_pil = y_pil.clamp(0, h - 1);

            let (r, g, b) = get_pixel(pixels, x, y_pil);
            let (r_b, g_b, b_b) = boost_saturation(r, g, b, boost);

            let mut idx = i;
            if i < half && reverse_first_half {
                idx = half - 1 - i;
            } else if i >= half && reverse_second_half {
                idx = half + (half - 1 - (i - half));
            }

            if reverse {
                idx = led_count - 1 - idx;
            }

            idx = ((idx as i32 + led_offset).rem_euclid(led_count as i32)) as usize;
            led_colors[idx] = (r_b, g_b, b_b);
        }
    } else {
        let p = (2 * (w + h)) as f32;
        for i in 0..led_count {
            let d = i as f32 * (p / led_count as f32);
            let mut x;
            let mut y_pil;

            if d < h as f32 {
                x = 0;
                y_pil = h - 1 - (d.round() as usize);
            } else if d < (h + w) as f32 {
                x = (d - h as f32).round() as usize;
                y_pil = 0;
            } else if d < (2 * h + w) as f32 {
                x = w - 1;
                y_pil = (d - (h + w) as f32).round() as usize;
            } else {
                x = ((2 * h + 2 * w) as f32 - 1.0 - d).round() as usize;
                y_pil = h - 1;
            }

            x = x.clamp(0, w - 1);
            y_pil = y_pil.clamp(0, h - 1);

            let (r, g, b) = get_pixel(pixels, x, y_pil);
            let (r_b, g_b, b_b) = boost_saturation(r, g, b, boost);

            let mut idx = i;
            if reverse {
                idx = led_count - 1 - i;
            }

            idx = ((idx as i32 + led_offset).rem_euclid(led_count as i32)) as usize;
            led_colors[idx] = (r_b, g_b, b_b);
        }
    }

    led_colors
}

/// Packs raw RGB light arrays into a WLED DRGB UDP packet and sends it on port 21324.
fn send_wled_udp(socket: &UdpSocket, ip: &str, colors: &[(u8, u8, u8)]) {
    let mut packet = Vec::with_capacity(2 + colors.len() * 3);
    packet.push(2); // DRGB Protocol Identifier
    packet.push(5); // Timeout (5 seconds)
    for &(r, g, b) in colors {
        packet.push(r);
        packet.push(g);
        packet.push(b);
    }
    let addr = format!("{}:21324", ip);
    if let Err(e) = socket.send_to(&packet, &addr) {
        eprintln!("Failed to send UDP packet: {}", e);
    }
}

// ── Direct Wayland Capture State & Protocol Handlers ────────────────────────

struct OutputEntry {
    registry_name: u32,
    wl_output: WlOutput,
    name: String,
}

#[derive(Default)]
struct SessionConstraints {
    size: Option<(u32, u32)>,
    dmabuf_device: Option<u64>,
    dmabuf_formats: Vec<u32>,
    done: bool,
}

enum FrameResult {
    Ready,
    Failed(WEnum<FailureReason>),
}

struct WaylandState {
    outputs: Vec<OutputEntry>,
    session_constraints: SessionConstraints,
    session_stopped: bool,
    frame_result: Option<FrameResult>,
}

impl WaylandState {
    fn new() -> Self {
        Self {
            outputs: Vec::new(),
            session_constraints: SessionConstraints::default(),
            session_stopped: false,
            frame_result: None,
        }
    }

    fn reset_session_events(&mut self) {
        self.session_constraints = SessionConstraints::default();
        self.session_stopped = false;
        self.frame_result = None;
    }

    fn find_output(&self, monitor_name: Option<&str>) -> Option<&WlOutput> {
        if let Some(name) = monitor_name {
            if let Some(entry) = self.outputs.iter().find(|o| o.name == name) {
                return Some(&entry.wl_output);
            }
        }
        self.outputs.first().map(|o| &o.wl_output)
    }
}

impl Dispatch<WlRegistry, GlobalListContents> for WaylandState {
    fn event(
        state: &mut Self,
        registry: &WlRegistry,
        event: wl_registry::Event,
        _: &GlobalListContents,
        _: &Connection,
        qh: &QueueHandle<Self>,
    ) {
        match event {
            wl_registry::Event::Global {
                name,
                interface,
                version,
            } if interface == "wl_output" => {
                let bind_version = version.min(4);
                let wl_output = registry.bind::<WlOutput, _, _>(name, bind_version, qh, ());
                state.outputs.push(OutputEntry {
                    registry_name: name,
                    wl_output,
                    name: String::new(),
                });
            }
            wl_registry::Event::GlobalRemove { name } => {
                state.outputs.retain(|o| o.registry_name != name);
            }
            _ => {}
        }
    }
}

impl Dispatch<WlOutput, ()> for WaylandState {
    fn event(
        state: &mut Self,
        wl_output: &WlOutput,
        event: wl_output::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let wl_output::Event::Name { name } = event {
            if let Some(entry) = state.outputs.iter_mut().find(|o| o.wl_output == *wl_output) {
                entry.name = name;
            }
        }
    }
}

impl Dispatch<ExtImageCopyCaptureSessionV1, ()> for WaylandState {
    fn event(
        state: &mut Self,
        _: &ExtImageCopyCaptureSessionV1,
        event: ext_image_copy_capture_session_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        match event {
            ext_image_copy_capture_session_v1::Event::BufferSize { width, height } => {
                state.session_constraints.size = Some((width, height));
            }
            ext_image_copy_capture_session_v1::Event::DmabufDevice { device } => {
                if let Ok(bytes) = <[u8; 8]>::try_from(device.as_slice()) {
                    state.session_constraints.dmabuf_device = Some(u64::from_le_bytes(bytes));
                }
            }
            ext_image_copy_capture_session_v1::Event::DmabufFormat { format, .. } => {
                if !state.session_constraints.dmabuf_formats.contains(&format) {
                    state.session_constraints.dmabuf_formats.push(format);
                }
            }
            ext_image_copy_capture_session_v1::Event::Done => {
                state.session_constraints.done = true;
            }
            ext_image_copy_capture_session_v1::Event::Stopped => {
                state.session_stopped = true;
            }
            _ => {}
        }
    }
}

impl Dispatch<ExtImageCopyCaptureFrameV1, ()> for WaylandState {
    fn event(
        state: &mut Self,
        _: &ExtImageCopyCaptureFrameV1,
        event: ext_image_copy_capture_frame_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        match event {
            ext_image_copy_capture_frame_v1::Event::Ready => {
                state.frame_result = Some(FrameResult::Ready);
            }
            ext_image_copy_capture_frame_v1::Event::Failed { reason } => {
                state.frame_result = Some(FrameResult::Failed(reason));
            }
            _ => {}
        }
    }
}

delegate_noop!(WaylandState: ignore ExtImageCopyCaptureManagerV1);
delegate_noop!(WaylandState: ignore ExtOutputImageCaptureSourceManagerV1);
delegate_noop!(WaylandState: ignore ExtImageCaptureSourceV1);
delegate_noop!(WaylandState: ignore ZwpLinuxDmabufV1);
delegate_noop!(WaylandState: ignore ZwpLinuxBufferParamsV1);
delegate_noop!(WaylandState: ignore WlBuffer);

/// RAII guard ensuring `ext_image_copy_capture_frame_v1.destroy()` is always called.
struct FrameGuard(ExtImageCopyCaptureFrameV1);

impl Drop for FrameGuard {
    fn drop(&mut self) {
        self.0.destroy();
    }
}

/// Persistent capture session + DMA-BUF + EGLImage for a single active monitor.
struct CaptureSession {
    source: ExtImageCaptureSourceV1,
    session: ExtImageCopyCaptureSessionV1,
    width: u32,
    height: u32,
    _gbm_device: gbm::Device<File>,
    _bo: gbm::BufferObject<()>,
    wl_buffer: WlBuffer,
    egl_display: egl::Display,
    egl_image: Option<egl::Image>,
    first_frame: bool,
}

impl Drop for CaptureSession {
    fn drop(&mut self) {
        if let Some(image) = self.egl_image.take() {
            EGL_INSTALCE.destroy_image(self.egl_display, image).ok();
        }
        self.wl_buffer.destroy();
        self.session.destroy();
        self.source.destroy();
    }
}

/// Resolves a DRM `dev_t` to its `/dev/dri/*` path, falling back to `/dev/dri/renderD128`.
fn resolve_drm_device_path(dev_id: Option<u64>) -> PathBuf {
    if let Some(target_rdev) = dev_id {
        if let Ok(entries) = std::fs::read_dir("/dev/dri") {
            for entry in entries.flatten() {
                if let Ok(meta) = entry.metadata() {
                    if meta.rdev() == target_rdev {
                        return entry.path();
                    }
                }
            }
        }
    }
    PathBuf::from("/dev/dri/renderD128")
}

/// Picks a preferred 8-bit DRM format if available, otherwise the first GBM-compatible format.
fn select_dmabuf_format(formats: &[u32]) -> Option<(u32, gbm::Format)> {
    const PREFERRED: [u32; 4] = [
        u32::from_le_bytes(*b"XR24"),
        u32::from_le_bytes(*b"AR24"),
        u32::from_le_bytes(*b"XB24"),
        u32::from_le_bytes(*b"AB24"),
    ];

    for &pref in &PREFERRED {
        if formats.contains(&pref) {
            if let Ok(gbm_fmt) = gbm::Format::try_from(pref) {
                return Some((pref, gbm_fmt));
            }
        }
    }

    for &fmt in formats {
        if let Ok(gbm_fmt) = gbm::Format::try_from(fmt) {
            return Some((fmt, gbm_fmt));
        }
    }

    None
}

/// Creates a persistent `EGLImage` from a `gbm::BufferObject` and binds it once to `src_tex`.
fn create_and_bind_egl_image(
    egl_display: egl::Display,
    bo: &gbm::BufferObject<()>,
    src_tex: gl::types::GLuint,
    src_w: u32,
    src_h: u32,
    format: u32,
) -> Result<egl::Image, String> {
    let fd = bo
        .fd_for_plane(0)
        .map_err(|e| format!("Failed to get DMA-BUF fd: {}", e))?;
    let raw_fd = fd.as_raw_fd();
    let modifier: u64 = bo.modifier().into();

    type Attrib = egl::Attrib;
    let image_attribs = [
        egl::WIDTH as Attrib,
        src_w as Attrib,
        egl::HEIGHT as Attrib,
        src_h as Attrib,
        egl::LINUX_DRM_FOURCC_EXT as Attrib,
        format as Attrib,
        egl::DMA_BUF_PLANE0_FD_EXT as Attrib,
        raw_fd as Attrib,
        egl::DMA_BUF_PLANE0_OFFSET_EXT as Attrib,
        bo.offset(0) as Attrib,
        egl::DMA_BUF_PLANE0_PITCH_EXT as Attrib,
        bo.stride_for_plane(0) as Attrib,
        egl::DMA_BUF_PLANE0_MODIFIER_LO_EXT as Attrib,
        (modifier as u32) as Attrib,
        egl::DMA_BUF_PLANE0_MODIFIER_HI_EXT as Attrib,
        (modifier >> 32) as Attrib,
        egl::ATTRIB_NONE as Attrib,
    ];

    unsafe {
        let image = EGL_INSTALCE
            .create_image(
                egl_display,
                egl::Context::from_ptr(egl::NO_CONTEXT),
                egl::LINUX_DMA_BUF_EXT as u32,
                egl::ClientBuffer::from_ptr(std::ptr::null_mut()),
                &image_attribs,
            )
            .map_err(|e| format!("eglCreateImage failed: {:?}", e))?;

        let f = EGL_INSTALCE
            .get_proc_address("glEGLImageTargetTexture2DOES")
            .ok_or_else(|| "glEGLImageTargetTexture2DOES not found".to_string())?;

        let gl_egl_image_target_texture_2d_oes: unsafe extern "system" fn(
            gl::types::GLenum,
            gl::types::GLeglImageOES,
        ) -> () = std::mem::transmute(f);

        gl::BindTexture(gl::TEXTURE_2D, src_tex);
        gl_egl_image_target_texture_2d_oes(gl::TEXTURE_2D, image.as_ptr());

        Ok(image)
    }
}

/// Polls the Wayland socket up to `timeout_ms` and dispatches any pending events.
fn poll_dispatch(
    event_queue: &mut EventQueue<WaylandState>,
    state: &mut WaylandState,
    timeout_ms: i32,
) -> Result<(), String> {
    event_queue
        .dispatch_pending(state)
        .map_err(|e| format!("dispatch_pending failed: {}", e))?;
    event_queue
        .flush()
        .map_err(|e| format!("flush failed: {}", e))?;

    if let Some(guard) = event_queue.prepare_read() {
        let fd = guard.connection_fd().as_raw_fd();
        let mut pfd = libc::pollfd {
            fd,
            events: libc::POLLIN,
            revents: 0,
        };
        let ret = unsafe { libc::poll(&mut pfd, 1, timeout_ms) };
        if ret < 0 {
            let err = std::io::Error::last_os_error();
            drop(guard);
            if err.kind() == std::io::ErrorKind::Interrupted {
                return Ok(());
            }
            return Err(format!("poll failed: {}", err));
        } else if ret > 0 {
            guard
                .read()
                .map_err(|e| format!("Wayland read failed: {}", e))?;
        } else {
            drop(guard);
        }
    }

    event_queue
        .dispatch_pending(state)
        .map_err(|e| format!("dispatch_pending failed: {}", e))?;
    Ok(())
}

/// Creates a persistent capture session for the selected monitor.
#[allow(clippy::too_many_arguments)]
fn init_capture_session(
    event_queue: &mut EventQueue<WaylandState>,
    qh: &QueueHandle<WaylandState>,
    state: &mut WaylandState,
    output_source_manager: &ExtOutputImageCaptureSourceManagerV1,
    capture_manager: &ExtImageCopyCaptureManagerV1,
    linux_dmabuf: &ZwpLinuxDmabufV1,
    egl_display: egl::Display,
    src_tex: gl::types::GLuint,
    monitor_name: Option<&str>,
) -> Result<CaptureSession, String> {
    let wl_output = state
        .find_output(monitor_name)
        .cloned()
        .ok_or_else(|| "No Wayland output available".to_string())?;

    state.reset_session_events();

    let source = output_source_manager.create_source(&wl_output, qh, ());
    let session = capture_manager.create_session(&source, CaptureOptions::empty(), qh, ());

    let deadline = Instant::now() + Duration::from_secs(5);
    while !state.session_constraints.done && !state.session_stopped {
        if Instant::now() >= deadline {
            session.destroy();
            source.destroy();
            return Err("Timed out waiting for capture session constraints".to_string());
        }
        poll_dispatch(event_queue, state, 200)?;
    }

    if state.session_stopped {
        session.destroy();
        source.destroy();
        return Err("Capture session stopped during initialization".to_string());
    }

    let (width, height) = state.session_constraints.size.ok_or_else(|| {
        session.destroy();
        source.destroy();
        "Missing buffer_size from capture session".to_string()
    })?;

    let (drm_format, gbm_format) =
        select_dmabuf_format(&state.session_constraints.dmabuf_formats).ok_or_else(|| {
            session.destroy();
            source.destroy();
            "No supported DMA-BUF format advertised by compositor".to_string()
        })?;

    let drm_path = resolve_drm_device_path(state.session_constraints.dmabuf_device);
    let drm_file = OpenOptions::new()
        .read(true)
        .write(true)
        .open(&drm_path)
        .map_err(|e| {
            session.destroy();
            source.destroy();
            format!("Failed to open DRM device {:?}: {}", drm_path, e)
        })?;

    let gbm_device = gbm::Device::new(drm_file).map_err(|e| {
        session.destroy();
        source.destroy();
        format!("Failed to create GBM device: {}", e)
    })?;

    let bo = gbm_device
        .create_buffer_object::<()>(
            width,
            height,
            gbm_format,
            BufferObjectFlags::RENDERING | BufferObjectFlags::LINEAR,
        )
        .map_err(|e| {
            session.destroy();
            source.destroy();
            format!("Failed to create GBM BufferObject: {}", e)
        })?;

    let fd = bo.fd_for_plane(0).map_err(|e| {
        session.destroy();
        source.destroy();
        format!("Failed to export DMA-BUF fd: {}", e)
    })?;
    let stride = bo.stride();
    let offset = bo.offset(0);
    let modifier: u64 = bo.modifier().into();

    let dma_params = linux_dmabuf.create_params(qh, ());
    dma_params.add(
        fd.as_fd(),
        0,
        offset,
        stride,
        (modifier >> 32) as u32,
        (modifier & 0xffffffff) as u32,
    );
    let wl_buffer = dma_params.create_immed(
        width as i32,
        height as i32,
        drm_format,
        zwp_linux_buffer_params_v1::Flags::empty(),
        qh,
        (),
    );
    dma_params.destroy();

    let egl_image = match create_and_bind_egl_image(
        egl_display,
        &bo,
        src_tex,
        width,
        height,
        drm_format,
    ) {
        Ok(img) => img,
        Err(e) => {
            wl_buffer.destroy();
            session.destroy();
            source.destroy();
            return Err(e);
        }
    };

    Ok(CaptureSession {
        source,
        session,
        width,
        height,
        _gbm_device: gbm_device,
        _bo: bo,
        wl_buffer,
        egl_display,
        egl_image: Some(egl_image),
        first_frame: true,
    })
}

fn main() {
    // ── Phase 1: Environment & Settings Loading ──────────────────────────────
    let env_map = get_env_map();
    let config = parse_args(&env_map);
    println!("Loaded Config: {:?}", config);

    if config.wled_ip.is_empty() {
        eprintln!("Error: WLED IP address is missing! Set WLED_SCHREIBTISCH_IP in your environment or pass --wled-ip.");
        std::process::exit(1);
    }

    // ── Phase 2: Headless EGL & OpenGL Context Initialization ────────────────
    let conn = Connection::connect_to_env().expect("Failed to connect to Wayland display");
    let wl_display = conn.display();

    let egl_display = EGL_INSTALCE.get_display_wl(&wl_display).unwrap();
    EGL_INSTALCE.initialize(egl_display).unwrap();

    let attributes = [egl::RENDERABLE_TYPE, egl::OPENGL_ES2_BIT, egl::NONE];
    let egl_config = EGL_INSTALCE
        .choose_first_config(egl_display, &attributes)
        .unwrap()
        .expect("unable to find an appropriate EGL configuration");

    let context_attributes = [egl::CONTEXT_CLIENT_VERSION, 3, egl::NONE];
    let egl_context = EGL_INSTALCE
        .create_context(egl_display, egl_config, None, &context_attributes)
        .unwrap();

    let pbuffer_attributes = [egl::WIDTH, 100, egl::HEIGHT, 100, egl::NONE];
    let egl_surface = EGL_INSTALCE
        .create_pbuffer_surface(egl_display, egl_config, &pbuffer_attributes)
        .ok();

    if egl_surface.is_some() {
        println!("Successfully created EGL pbuffer surface.");
        EGL_INSTALCE
            .make_current(egl_display, egl_surface, egl_surface, Some(egl_context))
            .unwrap();
    } else {
        println!("Failed to create Pbuffer surface. Trying surfaceless EGL context...");
        EGL_INSTALCE
            .make_current(egl_display, None, None, Some(egl_context))
            .unwrap();
    }

    gl::load_with(|s| match EGL_INSTALCE.get_proc_address(s) {
        Some(f) => f as *const c_void,
        None => std::ptr::null(),
    });

    println!("EGL + headless GL initialized successfully!");

    // ── Phase 3: Bind Wayland Manager Globals Once ───────────────────────────
    let (globals, mut event_queue) =
        registry_queue_init::<WaylandState>(&conn).expect("Failed to initialize Wayland registry");
    let qh = event_queue.handle();
    let mut wayland_state = WaylandState::new();

    let capture_manager: ExtImageCopyCaptureManagerV1 = globals
        .bind(&qh, 1..=1, ())
        .expect("Compositor does not support ext_image_copy_capture_manager_v1");
    let output_source_manager: ExtOutputImageCaptureSourceManagerV1 = globals
        .bind(&qh, 1..=1, ())
        .expect("Compositor does not support ext_output_image_capture_source_manager_v1");
    let linux_dmabuf: ZwpLinuxDmabufV1 = globals
        .bind(&qh, 4..=ZwpLinuxDmabufV1::interface().version, ())
        .expect("Compositor does not support zwp_linux_dmabuf_v1 v4+");

    globals.contents().with_list(|list| {
        for global in list {
            if global.interface == "wl_output" {
                let bind_version = global.version.min(4);
                let wl_output = globals.registry().bind::<WlOutput, _, _>(
                    global.name,
                    bind_version,
                    &qh,
                    (),
                );
                wayland_state.outputs.push(OutputEntry {
                    registry_name: global.name,
                    wl_output,
                    name: String::new(),
                });
            }
        }
    });

    event_queue
        .roundtrip(&mut wayland_state)
        .expect("Initial Wayland roundtrip failed");

    // ── Phase 4: Setup OpenGL Textures & Framebuffer Objects (FBO) ───────────
    let mut src_tex: gl::types::GLuint = 0;
    let mut src_fbo: gl::types::GLuint = 0;
    let mut dst_tex: gl::types::GLuint = 0;
    let mut dst_fbo: gl::types::GLuint = 0;

    unsafe {
        gl::GenTextures(1, &mut src_tex);
        gl::BindTexture(gl::TEXTURE_2D, src_tex);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MIN_FILTER, gl::LINEAR as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MAG_FILTER, gl::LINEAR as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_WRAP_S, gl::CLAMP_TO_EDGE as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_WRAP_T, gl::CLAMP_TO_EDGE as i32);

        gl::GenFramebuffers(1, &mut src_fbo);
        gl::BindFramebuffer(gl::FRAMEBUFFER, src_fbo);
        gl::FramebufferTexture2D(
            gl::FRAMEBUFFER,
            gl::COLOR_ATTACHMENT0,
            gl::TEXTURE_2D,
            src_tex,
            0,
        );

        gl::GenTextures(1, &mut dst_tex);
        gl::BindTexture(gl::TEXTURE_2D, dst_tex);
        gl::TexImage2D(
            gl::TEXTURE_2D,
            0,
            gl::RGBA as i32,
            100,
            100,
            0,
            gl::RGBA,
            gl::UNSIGNED_BYTE,
            std::ptr::null(),
        );
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MIN_FILTER, gl::LINEAR as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MAG_FILTER, gl::LINEAR as i32);

        gl::GenFramebuffers(1, &mut dst_fbo);
        gl::BindFramebuffer(gl::FRAMEBUFFER, dst_fbo);
        gl::FramebufferTexture2D(
            gl::FRAMEBUFFER,
            gl::COLOR_ATTACHMENT0,
            gl::TEXTURE_2D,
            dst_tex,
            0,
        );
    }

    // ── Phase 5: Low-Latency Network Socket & Main Loop ──────────────────────
    let socket = UdpSocket::bind("0.0.0.0:0").expect("Failed to bind UDP socket");
    socket.set_nonblocking(true).ok();

    let mut monitor_cache = MonitorCache::new();
    let mut active_monitor: Option<String> = None;
    let mut capture_session: Option<CaptureSession> = None;

    let mut pixels = vec![0u8; 100 * 100 * 4];
    let mut last_colors = vec![(0, 0, 0); config.led_count];
    let mut has_sent_frame = false;
    let mut last_send_time = Instant::now();

    println!(
        "Starting main high-performance loop with delay: {}ms",
        config.interval_ms
    );

    loop {
        let current_monitor = if let Some(m) = &config.monitor {
            Some(m.clone())
        } else {
            monitor_cache.get_focused()
        };

        if current_monitor != active_monitor || capture_session.is_none() {
            println!("Active monitor changed/initialized to {:?}", current_monitor);
            capture_session = None;
            let _ = event_queue.flush();
            active_monitor = current_monitor.clone();

            match init_capture_session(
                &mut event_queue,
                &qh,
                &mut wayland_state,
                &output_source_manager,
                &capture_manager,
                &linux_dmabuf,
                egl_display,
                src_tex,
                active_monitor.as_deref(),
            ) {
                Ok(session) => {
                    println!(
                        "Successfully initialized capture session ({}x{}).",
                        session.width, session.height
                    );
                    capture_session = Some(session);
                }
                Err(e) => {
                    eprintln!(
                        "Failed to initialize capture session for monitor {:?}: {}. Retrying in 1s...",
                        active_monitor, e
                    );
                    std::thread::sleep(Duration::from_secs(1));
                    continue;
                }
            }
        }

        let session = capture_session.as_mut().unwrap();
        let frame_start = Instant::now();

        wayland_state.frame_result = None;
        let frame_guard = FrameGuard(session.session.create_frame(&qh, ()));
        frame_guard.0.attach_buffer(&session.wl_buffer);
        if session.first_frame {
            frame_guard
                .0
                .damage_buffer(0, 0, session.width as i32, session.height as i32);
            session.first_frame = false;
        }
        frame_guard.0.capture();

        if let Err(e) = event_queue.flush() {
            eprintln!("Wayland flush failed: {}. Re-initializing...", e);
            drop(frame_guard);
            capture_session = None;
            std::thread::sleep(Duration::from_millis(200));
            continue;
        }

        let mut monitor_switched = false;
        while wayland_state.frame_result.is_none() && !wayland_state.session_stopped {
            if let Err(e) = poll_dispatch(&mut event_queue, &mut wayland_state, 200) {
                eprintln!("Wayland dispatch failed: {}", e);
                wayland_state.session_stopped = true;
                break;
            }

            // Keep WLED alive during completely static screens so its 5s UDP timeout never triggers.
            if has_sent_frame && last_send_time.elapsed() >= Duration::from_millis(200) {
                send_wled_udp(&socket, &config.wled_ip, &last_colors);
                last_send_time = Instant::now();
            }

            // Allow monitor focus switches to interrupt a static-screen wait.
            if config.monitor.is_none() {
                let polled_monitor = monitor_cache.get_focused();
                if polled_monitor != active_monitor {
                    monitor_switched = true;
                    break;
                }
            }
        }

        // Explicitly destroy the ext_image_copy_capture_frame_v1 object after every frame.
        drop(frame_guard);
        let _ = event_queue.flush();

        if monitor_switched {
            capture_session = None;
            continue;
        }

        if wayland_state.session_stopped {
            eprintln!("Capture session stopped by compositor. Re-initializing...");
            capture_session = None;
            std::thread::sleep(Duration::from_millis(200));
            continue;
        }

        match wayland_state.frame_result.take() {
            Some(FrameResult::Ready) => {}
            Some(FrameResult::Failed(reason)) => {
                eprintln!("Frame capture failed ({:?}). Re-initializing...", reason);
                capture_session = None;
                std::thread::sleep(Duration::from_millis(200));
                continue;
            }
            None => continue,
        }

        // Hardware-accelerated downscale from full-res DMA-BUF texture to 100x100 RGBA.
        unsafe {
            gl::BindFramebuffer(gl::READ_FRAMEBUFFER, src_fbo);
            gl::BindFramebuffer(gl::DRAW_FRAMEBUFFER, dst_fbo);
            gl::BlitFramebuffer(
                0,
                0,
                session.width as i32,
                session.height as i32,
                0,
                0,
                100,
                100,
                gl::COLOR_BUFFER_BIT,
                gl::LINEAR,
            );

            gl::BindFramebuffer(gl::READ_FRAMEBUFFER, dst_fbo);
            gl::ReadPixels(
                0,
                0,
                100,
                100,
                gl::RGBA,
                gl::UNSIGNED_BYTE,
                pixels.as_mut_ptr() as *mut c_void,
            );
        }

        let colors = extract_individual_led_colors(
            &pixels,
            config.led_count,
            config.boost,
            config.reverse,
            config.led_offset,
            &config.mapping,
            config.reverse_first_half,
            config.reverse_second_half,
        );

        let mut is_changed = true;
        if has_sent_frame && config.threshold > 0 && last_colors.len() == colors.len() {
            let mut max_diff = 0;
            for (c1, c2) in last_colors.iter().zip(colors.iter()) {
                let d_r = (c1.0 as i32 - c2.0 as i32).abs();
                let d_g = (c1.1 as i32 - c2.1 as i32).abs();
                let d_b = (c1.2 as i32 - c2.2 as i32).abs();
                max_diff = max_diff.max(d_r).max(d_g).max(d_b);
            }

            if max_diff <= config.threshold && last_send_time.elapsed() < Duration::from_millis(200)
            {
                is_changed = false;
            }
        }

        if is_changed {
            send_wled_udp(&socket, &config.wled_ip, &colors);
            last_colors = colors;
            last_send_time = Instant::now();
            has_sent_frame = true;
        }

        let elapsed = frame_start.elapsed();
        let target = Duration::from_millis(config.interval_ms);
        if elapsed < target {
            std::thread::sleep(target - elapsed);
        }
    }
}
