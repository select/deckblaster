use std::collections::HashMap;
use std::ffi::c_void;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::net::UdpSocket;
use std::path::Path;
use std::time::{Duration, Instant};

use wayland_client::Connection;
use r_egl_wayland::{WayEglTrait, EGL_INSTALCE, r_egl as egl};
use libwayshot::{WayshotConnection, WayshotTarget};

/// Configuration structure holding all parsed parameters for the Ambilight daemon.
#[derive(Debug)]
struct AppConfig {
    /// The network IP address of the WLED (Wireless Light Emitting Diode) strip.
    wled_ip: String,
    /// The physical number of Light Emitting Diodes (LEDs) on your desk light strip.
    led_count: usize,
    /// Saturation boost factor (e.g., 1.4 raises color intensity by 40%).
    boost: f32,
    /// Loop delay interval in milliseconds (e.g., 16ms target matches 60 Frames Per Second).
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

/// JSON (JavaScript Object Notation) representation of a single monitor queried from the Hyprland compositor.
#[derive(serde::Deserialize, Debug)]
struct HyprlandMonitor {
    /// The unique system name of the monitor (e.g., "DP-2", "eDP-1").
    name: String,
    /// True if this monitor currently has active user focus.
    focused: bool,
}

/// A lightweight cache structure to store the active monitor name for up to 2 seconds.
/// This prevents spamming the desktop compositor with shell command queries on every single frame.
struct MonitorCache {
    name: Option<String>,
    last_check: Option<Instant>,
}

impl MonitorCache {
    fn new() -> Self {
        Self { name: None, last_check: None }
    }

    /// Retrieves the name of the currently focused monitor.
    /// Uses cached results if the last query was less than 2 seconds ago.
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

/// Shells out to `hyprctl monitors -j` (JSON format) to identify which screen is focused.
fn get_focused_monitor() -> Option<String> {
    let output = std::process::Command::new("hyprctl")
        .args(&["monitors", "-j"])
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

/// Reads a standard environment config file line-by-line, parsing "KEY=VALUE" pairs
/// while ignoring comments starting with `#` and trimming surrounding quotation marks.
fn load_env_file<P: AsRef<Path>>(path: P, env_map: &mut HashMap<String, String>) {
    if let Ok(file) = File::open(path) {
        let reader = BufReader::new(file);
        for line in reader.lines() {
            if let Ok(line) = line {
                let trimmed = line.trim();
                if trimmed.is_empty() || trimmed.starts_with('#') {
                    continue;
                }
                if let Some(pos) = trimmed.find('=') {
                    let k = trimmed[..pos].trim().to_string();
                    let mut v = trimmed[pos + 1..].trim().to_string();
                    if (v.starts_with('"') && v.ends_with('"')) || (v.starts_with('\'') && v.ends_with('\'')) {
                        if v.len() >= 2 {
                            v = v[1..v.len() - 1].to_string();
                        }
                    }
                    env_map.insert(k, v);
                }
            }
        }
    }
}

/// Automatically searches for and merges configurations from both `deckblaster.env` and `streamdeck.env`
/// located under your user's home configuration directory (`~/.config/`).
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

/// Parses CLI (Command Line Interface) arguments and merges them with configurations loaded from environment files.
/// CLI flags always take highest precedence and override environment-defined variables.
fn parse_args(env: &HashMap<String, String>) -> AppConfig {
    let mut wled_ip = env.get("WLED_SCHREIBTISCH_IP").cloned().unwrap_or_default();
    let mut led_count = env.get("AMBILIGHT_LED_COUNT")
        .and_then(|v| v.parse().ok())
        .unwrap_or(80);
    let mut boost = 1.4f32;
    let mut interval_sec = 0.016f32; // Default to a smooth 16ms (60 Frames Per Second target)
    let mut mapping = env.get("AMBILIGHT_MAPPING").cloned().unwrap_or_else(|| "perimeter".to_string());
    let mut reverse = env.get("AMBILIGHT_REVERSE")
        .map(|v| v.to_lowercase() == "true")
        .unwrap_or(false);
    let mut led_offset = env.get("AMBILIGHT_LED_OFFSET")
        .and_then(|v| v.parse().ok())
        .unwrap_or(0);
    let mut reverse_first_half = env.get("AMBILIGHT_REVERSE_FIRST_HALF")
        .map(|v| v.to_lowercase() == "true")
        .unwrap_or(false);
    let mut reverse_second_half = env.get("AMBILIGHT_REVERSE_SECOND_HALF")
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
                    interval_sec = args[i + 1].parse().unwrap_or(0.016);
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

/// Matches a text-based monitor name (e.g. "DP-2") against Wayland display outputs
/// and returns a standard Wayland capture target wrapper.
fn find_output_target(wayshot: &WayshotConnection, monitor_name: &str) -> Option<WayshotTarget> {
    for output in wayshot.get_all_outputs() {
        if output.name == monitor_name {
            return Some(WayshotTarget::Screen(output.wl_output.clone()));
        }
    }
    None
}

/// Falls back to retrieving the very first available Wayland display output target.
fn get_default_target(wayshot: &WayshotConnection) -> Option<WayshotTarget> {
    wayshot.get_all_outputs()
        .first()
        .map(|o| WayshotTarget::Screen(o.wl_output.clone()))
}

/// Converts Red, Green, Blue (RGB) color channels (scaled between 0.0 and 1.0)
/// into Hue, Saturation, Value (HSV) color coordinates.
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

/// Converts Hue, Saturation, Value (HSV) color coordinates (scaled between 0.0 and 1.0)
/// back into standard Red, Green, Blue (RGB) channels.
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
/// This transforms the color into HSV space, multiplies the Saturation ('S') channel
/// by the boost factor, clamps the result safely to 1.0 max, and converts it back.
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

/// Reads a single pixel from our 1D row-major raw OpenGL pixel array.
/// Note on Coordinate Alignment:
///   * Standard graphics packages (like PIL / Pillow in Python) place (0,0) at the TOP-LEFT of an image.
///   * OpenGL places (0,0) at the BOTTOM-LEFT of the viewport.
///   * This translates PIL's coordinate `y_pil` into OpenGL's viewport coordinate `99 - y_pil`
///     so our border mapping coordinates match the screen layout perfectly!
fn get_pixel(pixels: &[u8], x: usize, y_pil: usize) -> (u8, u8, u8) {
    let y_gl = 99 - y_pil;
    let idx = (y_gl * 100 + x) * 4;
    (pixels[idx], pixels[idx + 1], pixels[idx + 2])
}

/// Traces the 100x100 downscaled frame borders and extracts individual RGB values for each physical LED.
/// Supports sequential "perimeter" clockwise traces and custom dual-segment symmetrical "split-up" configurations.
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
        // Dual symmetrical layout mapping:
        //   * Segment 1 (Left): Traces bottom-left corner (0, 99) up to top-middle (50, 0).
        //   * Segment 2 (Right): Traces bottom-right corner (99, 99) up to top-middle (50, 0).
        let half = led_count / 2;
        let divisor = if half > 1 { half - 1 } else { 1 } as f32;
        let path_len = 150.0f32; // Total virtual trace length (100 units up + 50 units across)

        for i in 0..led_count {
            let mut x;
            let mut y_pil;

            if i < half {
                // Segment 1 (Left Half)
                let d = i as f32 * (path_len / divisor);
                if d < h as f32 {
                    x = 0;
                    y_pil = h - 1 - (d.round() as usize);
                } else {
                    x = (d - h as f32).round() as usize;
                    y_pil = 0;
                }
            } else {
                // Segment 2 (Right Half)
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
            // Apply segment-specific direction reversals if requested in configuration
            if i < half && reverse_first_half {
                idx = half - 1 - i;
            } else if i >= half && reverse_second_half {
                idx = half + (half - 1 - (i - half));
            }

            if reverse {
                idx = led_count - 1 - idx;
            }

            // Align starting LED on the physical light strip with the screen corner using rem_euclid (modulo)
            idx = ((idx as i32 + led_offset).rem_euclid(led_count as i32)) as usize;
            led_colors[idx] = (r_b, g_b, b_b);
        }
    } else {
        // Standard full-perimeter clockwise trace starting from bottom-left (0, 99).
        let p = (2 * (w + h)) as f32; // Total perimeter: 400 units
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

/// Packs raw RGB light arrays into a real-time WLED UDP packet payload
/// and sends it to the destination strip on port 21324.
/// 
/// Protocol Details:
///   * Byte 0: 2 (Specifies WLED's "DRGB" / Direct RGB UDP protocol mode)
///   * Byte 1: 5 (Command timeout in seconds; tells WLED to revert to standard patterns if no new packet arrives for 5 seconds)
///   * Bytes 2+: Spliced raw RGB byte triplets
fn send_wled_udp(socket: &UdpSocket, ip: &str, colors: &[(u8, u8, u8)]) {
    let mut packet = Vec::with_capacity(2 + colors.len() * 3);
    packet.push(2); // DRGB Protocol Identifier
    packet.push(5); // Timeout (5 seconds for resilience against UDP packet drops)
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

/// Initializes a fresh Wayland screencast session.
/// Allocates memory buffers backed directly in VRAM via DMA-BUF (Direct Memory Access Buffer)
/// which allows shared access between the Wayland display compositor and our graphics driver without slow VRAM-to-RAM copy cycles.
fn init_screencast(
    conn: &Connection,
    monitor_name: Option<&str>,
) -> Option<(libwayshot::WayshotConnection, libwayshot::screencast::WayshotScreenCast)> {
    // Obtain Wayshot connection linked to the Direct Rendering Manager (DRM) render node.
    let wayshot = libwayshot::WayshotConnection::from_connection_with_dmabuf(
        conn.clone(),
        "/dev/dri/renderD128",
    ).ok()?;
    
    // Resolve capturing target
    let target = if let Some(m) = monitor_name {
        find_output_target(&wayshot, m)
    } else {
        None
    }.or_else(|| get_default_target(&wayshot))?;

    // Create persistent screencast tied to DMA-BUF.
    // Specifying None for the EGL display parameter here is crucial:
    // This blocks libwayshot's internal, leaky EGL image wrapper, allowing us to manage
    // EGLImage creation and file descriptors manually and safely in main.rs!
    let cast = wayshot.create_screencast_with_dmabuf(
        target,
        false, // cursor_overlay (disable mouse rendering on lights)
        None, // capture_region (full-screen)
    ).ok()?;

    Some((wayshot, cast))
}

/// Manually binds our GPU-backed DMA-BUF (Direct Memory Access Buffer) frame to an active OpenGL texture.
/// 
/// ⚠️ Graphic Systems & Resource Allocation Explained:
///   * EGL (Embedded-System Graphics Library) is the platform glue layers connecting OpenGL rendering
///     APIs with underlying display servers (Wayland) and drivers.
///   * GBM (Generic Buffer Management) provides API structures for memory buffers managed by Mesa/Nvidia drivers.
///   * This function fetches the underlying Linux File Descriptor (FD) representing the shared VRAM plane.
///   * We pass the FD as a temporary `AsRawFd` reference to the GPU via EGL (`eglCreateImage`).
///   * Crucially, we wrap the FD in Rust's safe `OwnedFd` type. By utilizing Rust's RAII (Resource Acquisition Is Initialization)
///     paradigm, the File Descriptor is guaranteed to be closed at the end of this block, preventing a massive file descriptor leak.
fn update_egl_texture(
    egl_display: egl::Display,
    bo: &gbm::BufferObject<()>,
    src_w: u32,
    src_h: u32,
    format: u32,
) -> Result<(), String> {
    use std::os::fd::AsRawFd;
    
    // Get the shared plane memory handle as a safe, RAII-backed OwnedFd
    let fd = bo.fd_for_plane(0).map_err(|e| format!("Failed to get fd: {}", e))?;
    let raw_fd = fd.as_raw_fd();
    let modifier: u64 = bo.modifier().into();
    
    type Attrib = egl::Attrib;
    // Parameter list for constructing the EGLImage from our raw hardware buffer
    let image_attribs = [
        egl::WIDTH as Attrib,
        src_w as Attrib,
        egl::HEIGHT as Attrib,
        src_h as Attrib,
        egl::LINUX_DRM_FOURCC_EXT as Attrib,
        format as Attrib,
        egl::DMA_BUF_PLANE0_FD_EXT as Attrib,
        raw_fd as Attrib, // Feed the file descriptor pointing to VRAM
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
        // 1. Create a platform-independent EGLImage handle from the raw buffer
        let image = EGL_INSTALCE
            .create_image(
                egl_display,
                egl::Context::from_ptr(egl::NO_CONTEXT),
                egl::LINUX_DMA_BUF_EXT as u32,
                egl::ClientBuffer::from_ptr(std::ptr::null_mut()),
                &image_attribs,
            )
            .map_err(|e| format!("eglCreateImage failed: {:?}", e))?;

        // 2. Fetch the OpenGL extension function pointer for binding EGLImages to standard GL textures
        let f = EGL_INSTALCE.get_proc_address("glEGLImageTargetTexture2DOES")
            .ok_or_else(|| "glEGLImageTargetTexture2DOES not found".to_string())?;
        
        let gl_egl_image_target_texture_2d_oes: unsafe extern "system" fn(gl::types::GLenum, gl::types::GLeglImageOES) -> () = 
            std::mem::transmute(f);
            
        // 3. Bind the EGLImage directly to our source OpenGL 2D texture (zero CPU copies!)
        gl_egl_image_target_texture_2d_oes(gl::TEXTURE_2D, image.as_ptr());

        // 4. Safely destroy the EGLImage wrapper (the GL texture automatically retains a reference to the buffer storage)
        EGL_INSTALCE.destroy_image(egl_display, image).ok();
    }
    
    // 💡 Magic of Rust RAII:
    // When the local `fd` variable goes out of scope here, Rust automatically closes the file descriptor handle,
    // solving the critical resource leak and ensuring infinite uptime!
    Ok(())
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

    // ── Phase 2: Headless EGL & OpenGL Context Initialization ──────────────────
    // Create a connection link to the Wayland Display compositor environment.
    let conn = Connection::connect_to_env().unwrap();
    let wl_display = conn.display();
    
    // Obtain and initialize an EGL display handle connected directly to Wayland.
    let egl_display = EGL_INSTALCE.get_display_wl(&wl_display).unwrap();
    EGL_INSTALCE.initialize(egl_display).unwrap();
    
    let attributes = [
        egl::RENDERABLE_TYPE,
        egl::OPENGL_ES2_BIT, // Matches OpenGL ES 2.0 / 3.0 render capabilities
        egl::NONE,
    ];

    // Find a matching physical EGL hardware layout configuration
    let egl_config = EGL_INSTALCE
        .choose_first_config(egl_display, &attributes)
        .unwrap()
        .expect("unable to find an appropriate EGL configuration");

    // Initialize OpenGL ES 3.0 Rendering Context (supports high-speed hardware blitting)
    let context_attributes = [egl::CONTEXT_CLIENT_VERSION, 3, egl::NONE];
    let egl_context =
        EGL_INSTALCE.create_context(egl_display, egl_config, None, &context_attributes).unwrap();

    // Create an offscreen rendering surface (Pbuffer / Pixel Buffer).
    // If the active driver does not support offscreen Pbuffers on Wayland,
    // we fallback to creating a completely surfaceless EGL context (EGL_NO_SURFACE).
    let pbuffer_attributes = [
        egl::WIDTH, 100,
        egl::HEIGHT, 100,
        egl::NONE,
    ];
    let egl_surface = EGL_INSTALCE.create_pbuffer_surface(egl_display, egl_config, &pbuffer_attributes).ok();

    if egl_surface.is_some() {
        println!("Successfully created EGL pbuffer surface.");
        EGL_INSTALCE.make_current(
            egl_display,
            egl_surface,
            egl_surface,
            Some(egl_context),
        ).unwrap();
    } else {
        println!("Failed to create Pbuffer surface. Trying surfaceless EGL context...");
        EGL_INSTALCE.make_current(
            egl_display,
            None,
            None,
            Some(egl_context),
        ).unwrap();
    }

    // Load active OpenGL ES function pointers dynamically from the EGL driver
    gl::load_with(|s| {
        match EGL_INSTALCE.get_proc_address(s) {
            Some(f) => f as *const std::ffi::c_void,
            None => std::ptr::null(),
        }
    });

    println!("EGL + headless GL initialized successfully!");

    // ── Phase 3: Setup OpenGL Textures & Framebuffer Objects (FBO) ────────────
    // Framebuffer Objects (FBOs) are off-screen targets in VRAM that allow rendering
    // directly to textures instead of onto the physical screen display.
    let mut src_tex: gl::types::GLuint = 0;
    let mut src_fbo: gl::types::GLuint = 0;
    let mut dst_tex: gl::types::GLuint = 0;
    let mut dst_fbo: gl::types::GLuint = 0;

    unsafe {
        // Create the Source Texture (holds the raw, full-scale 4K screen capture frame)
        gl::GenTextures(1, &mut src_tex);
        gl::BindTexture(gl::TEXTURE_2D, src_tex);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MIN_FILTER, gl::LINEAR as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_MAG_FILTER, gl::LINEAR as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_WRAP_S, gl::CLAMP_TO_EDGE as i32);
        gl::TexParameteri(gl::TEXTURE_2D, gl::TEXTURE_WRAP_T, gl::CLAMP_TO_EDGE as i32);

        // Attach Source Texture to the Source Framebuffer (src_fbo)
        gl::GenFramebuffers(1, &mut src_fbo);
        gl::BindFramebuffer(gl::FRAMEBUFFER, src_fbo);
        gl::FramebufferTexture2D(
            gl::FRAMEBUFFER,
            gl::COLOR_ATTACHMENT0,
            gl::TEXTURE_2D,
            src_tex,
            0,
        );

        // Create the Destination Texture (a static, tiny 100x100 texture)
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

        // Attach Destination Texture to our Destination Framebuffer (dst_fbo)
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

    // ── Phase 4: Low-Latency Network Socket ──────────────────────────────────
    let socket = UdpSocket::bind("0.0.0.0:0").expect("Failed to bind UDP socket");
    socket.set_nonblocking(true).ok();

    let mut monitor_cache = MonitorCache::new();
    let mut active_monitor: Option<String> = None;
    let mut wayshot_and_cast: Option<(WayshotConnection, libwayshot::screencast::WayshotScreenCast)> = None;

    let mut last_colors = vec![(0, 0, 0); config.led_count];
    let mut last_send_time = Instant::now();

    println!("Starting main high-performance loop with delay: {}ms", config.interval_ms);

    // ── Main High-Performance Capture and Render Loop ────────────────────────
    loop {
        // 1. Identify which monitor is currently active/focused
        let current_monitor = if let Some(m) = &config.monitor {
            Some(m.clone())
        } else {
            monitor_cache.get_focused()
        };

        // 2. Self-Healing Monitor focus switching:
        // Re-creates the screencast handles if the active monitor changes or is lost.
        if current_monitor != active_monitor || wayshot_and_cast.is_none() {
            println!("Active monitor changed/initialized to {:?}", current_monitor);
            active_monitor = current_monitor.clone();
            wayshot_and_cast = init_screencast(&conn, active_monitor.as_deref());
            if wayshot_and_cast.is_none() {
                eprintln!("Failed to initialize screencast for monitor: {:?}. Retrying in 1s...", active_monitor);
                std::thread::sleep(Duration::from_secs(1));
                continue;
            }
            println!("Successfully initialized screencast.");
        }

        let (wayshot, cast) = wayshot_and_cast.as_mut().unwrap();

        // 3. Bind our source texture before capturing so EGL binds the frame directly to it
        unsafe {
            gl::BindTexture(gl::TEXTURE_2D, src_tex);
        }

        // 4. Capture the screen frame directly into GPU VRAM (DMA-BUF)
        if let Err(e) = wayshot.screencast(cast) {
            eprintln!("Screencast failed: {}. Re-initializing...", e);
            std::thread::sleep(Duration::from_millis(500));
            wayshot_and_cast = None;
            continue;
        }

        // 5. Update our OpenGL texture mapping leak-free
        let bo = cast.dmabuf_bo().expect("No DMA-BUF buffer object");
        let src_w = bo.width();
        let src_h = bo.height();
        let format = bo.format() as u32;

        if let Err(err_msg) = update_egl_texture(egl_display, bo, src_w, src_h, format) {
            eprintln!("Failed to update EGL texture: {}. Re-initializing...", err_msg);
            std::thread::sleep(Duration::from_millis(500));
            wayshot_and_cast = None;
            continue;
        }

        // 6. Zero-Copy Hardware Scaling (BlitFramebuffer):
        // Blits from our full-scale source framebuffer (3840x2160) to our tiny 100x100 destination framebuffer.
        // The GPU scales the image in less than 0.1ms using highly optimized linear interpolation hardware!
        unsafe {
            gl::BindFramebuffer(gl::READ_FRAMEBUFFER, src_fbo);
            gl::BindFramebuffer(gl::DRAW_FRAMEBUFFER, dst_fbo);
            gl::BlitFramebuffer(
                0, 0, src_w as i32, src_h as i32,
                0, 0, 100, 100,
                gl::COLOR_BUFFER_BIT,
                gl::LINEAR,
            );
        }

        // 7. Transfer the tiny 30KB buffer (100x100 RGBA) to system RAM (taking <0.05ms)
        let mut pixels = vec![0u8; 100 * 100 * 4];
        unsafe {
            gl::BindFramebuffer(gl::READ_FRAMEBUFFER, dst_fbo);
            gl::ReadPixels(
                0, 0, 100, 100,
                gl::RGBA,
                gl::UNSIGNED_BYTE,
                pixels.as_mut_ptr() as *mut c_void,
            );
        }

        // 8. Trace the borders of our 100x100 frame and map them to physical desk LEDs
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

        // 9. Smart Noise/Network Smoothing logic:
        // Compares colors with the last sent frame. If changes are below our threshold,
        // we skip sending to save substantial network traffic. We still send a keepalive packet
        // once every 200ms so WLED does not timeout and revert to default patterns under wifi packet drops.
        let mut is_changed = true;
        if config.threshold > 0 && last_colors.len() == colors.len() {
            let mut max_diff = 0;
            for (c1, c2) in last_colors.iter().zip(colors.iter()) {
                let d_r = (c1.0 as i32 - c2.0 as i32).abs();
                let d_g = (c1.1 as i32 - c2.1 as i32).abs();
                let d_b = (c1.2 as i32 - c2.2 as i32).abs();
                max_diff = max_diff.max(d_r).max(d_g).max(d_b);
            }

            if max_diff <= config.threshold {
                if last_send_time.elapsed() < Duration::from_millis(200) {
                    is_changed = false;
                }
            }
        }

        // 10. Fire the UDP packet to the physical lights
        if is_changed {
            send_wled_udp(&socket, &config.wled_ip, &colors);
            last_colors = colors;
            last_send_time = Instant::now();
        }

        // 11. Sleep for our target interval (e.g. 16ms for a silky smooth 60 Frames Per Second Ambilight sync)
        std::thread::sleep(Duration::from_millis(config.interval_ms));
    }
}
