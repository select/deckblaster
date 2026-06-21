# 📋 Implementation Plan: GPU-Accelerated Rust Ambilight Daemon

This daemon completely bypasses standard screenshot capturing, file descriptor passing, and CPU image processing. Instead, it uses **Wayland DMA-BUF**, headless **EGL**, and hardware-driven **OpenGL Framebuffer Blitting** to downscale the screen frame in VRAM before copying only a tiny 30KB buffer to system memory.

---

## 🛠️ Phase 1: Environment & CLI Parser
We will write a robust configuration loader in Rust that:
1. Loads `.env` configurations from `~/.config/deckblaster.env` and `~/.config/streamdeck.env`.
2. Parses CLI flags (or falls back to environment variables) for:
   * `--wled-ip` (e.g., `192.168.1.188`)
   * `--led-count` (default: `80`)
   * `--boost` (default: `1.4` saturation boost)
   * `--interval` (default: `0.016` seconds for a smooth 60 FPS target!)
   * `--mapping` (`perimeter` or `split-up` matching your desk segments)
   * `--reverse`, `--led-offset`, `--reverse-first-half`, `--reverse-second-half`

---

## 🖥️ Phase 2: Headless EGL & OpenGL Pipeline
To perform hardware-accelerated downscaling without opening a window on your desktop:
1. **Wayland Display Link:** Create a persistent connection to your active Wayland compositor environment using `Connection::connect_to_env()`.
2. **Headless EGL Context:**
   * Get an EGL display connection bound to your Wayland display using NVIDIA/Mesa client extensions.
   * Bind an EGL configuration supporting **Pbuffers** (Pixel Buffers) and OpenGL ES.
   * Create a headless, off-screen `100x100` EGL Pbuffer Surface (`create_pbuffer_surface`) as our active drawing target.
   * Initialize and make the context current.
3. **OpenGL Loading:** Bind OpenGL function pointers dynamically using EGL's `get_proc_address`.

---

## 🚀 Phase 3: Zero-Copy Screencast & VRAM Blitting
Instead of allocating 33MB of CPU memory per frame, we'll keep the image on your GPU:
1. **DMA-BUF Initialization:** Bind to Wayland's `linux-dmabuf` protocol globals to allow direct GPU memory sharing.
2. **Persistent Screencast:** Set up a permanent screen capture stream using `libwayshot::create_screencast_with_egl`. This binds the live screen frame directly to a source GPU texture (`gl_texture`) in VRAM.
3. **Hardware-Accelerated Blit Unit (`BlitFramebuffer`):**
   * Create a **Source Framebuffer (`src_fbo`)** and attach the raw screen texture (3840x2160) to it.
   * Create a **Destination Framebuffer (`dst_fbo`)** and attach a small `100x100` texture to it.
   * In the main loop:
     * Call `wayshot.screencast(&mut cast)` to let the graphics driver update the texture on the GPU.
     * Execute `gl::BlitFramebuffer` with `gl::LINEAR` filtering to blit from `src_fbo` (3840x2160) to `dst_fbo` (100x100). The GPU scales the full 4K screen down in **less than 0.1ms** using dedicated graphics hardware.
     * Call `gl::ReadPixels` to transfer **only the final 100x100 pixels (just 30 Kilobytes!)** from the GPU to System RAM (taking `<0.05ms`).

---

## 🎨 Phase 4: Fast RGB Processing & Desk LED Mapping
Now that we have the tiny `100x100` RGBA array in System memory:
1. **Boost Saturation:** Convert the border pixels to HSV on the fly, apply the saturation boost factor (e.g., `1.4`), and convert back to RGB.
2. **Physical Desk Mapping:**
   * **`perimeter`:** Trace the edge pixels of the 100x100 grid clockwise.
   * **`split-up`:** Map the bottom-to-top segments symmetrically (Segment 1 left side bottom-up, Segment 2 right side bottom-up, with segment-specific reversing).
3. **Smooth Out Noise:** Integrate a configurable color change threshold so we don't spam the network with UDP packets if colors have changed by less than 1-2 units.

---

## 🛰️ Phase 5: Direct Low-Latency UDP Streaming
1. Open a non-blocking `std::net::UdpSocket`.
2. Format the payload into WLED's real-time **DRGB (Direct RGB) protocol packet** (Protocol Byte `2`, Timeout Byte `2`, followed by the raw RGB sequence).
3. Directly fire the packet to `WLED_SCHREIBTISCH_IP:21324`.

---

## 📦 Phase 6: System Integration & Stream Deck Toggle
1. **Compilation:** Build the Rust executable in `--release` mode, stripping debug symbols for a tiny, highly-optimized binary.
2. **Stream Deck Update:** Update the background starter script (`ambilight-toggle.py`) to launch our compiled high-performance Rust binary instead of the Python script.
