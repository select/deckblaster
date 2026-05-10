# Stream Deck Alternatives

Hardware alternatives to the Elgato Stream Deck — macro pads / button controllers with LCD keys.

---

## Devices

### VSDinside Stream Dock N1

- **Price**: ~$69.99 USD
- **Link**: [vsdinside.com — Stream Dock N1](https://www.vsdinside.com/collections/streamdock/products/vsd-stream-dock-n1)
- **Features**:
  - GIF / animated dynamic icons
  - Screenshot-as-icon support
  - Long-press continuous trigger
  - Scene Follow (key values switch with software profile)
  - Operation flow — open multiple programs with one button
  - 300+ themes & scenes, 400+ free plugins
  - SDK compatible with Stream Deck SDK
- **Software**: proprietary VSDinside app (Windows / macOS)
- **Linux**: ❌ no official Linux support

---

### Ulanzi Stream Controller D200

- **Price**: available from [ulanzi.de](https://www.ulanzi.de/products/stream-controller-d200?redirected=true)
- **Link**: [ulanzi.de — Stream Controller D200](https://www.ulanzi.de/products/stream-controller-d200?redirected=true)
- **Specs**:
  - Body: glass + aluminium alloy + plastic
  - USB 2.0 (USB-A to USB-C cable)
  - Dimensions: 15.4 × 9.2 × 7.8 cm
  - Weight: 230 g (net) / 666 g (gross)
- **Features**:
  - Sound effects, GIF icons, lighting control
  - Macro shortcuts, Zoom/OBS/Spotify/Twitch/TikTok/YouTube integrations
  - Marketplace for plugins, profiles, and icons (`ugc.ulanzistudio.com`)
  - Regular content updates + developer SDK
- **Software**: Ulanzi Deck — download at `https://www.ulanzi.com/pages/downloads`
- **Supported OS**: Windows 7+ / macOS 10.13+
- **Linux**: ❌ no official Linux support

---

## Notes

- Neither device has official Linux support — they rely on proprietary Windows/macOS apps.
- The underlying USB HID protocol is likely compatible with [python-elgato-streamdeck](https://github.com/abcminiuser/python-elgato-streamdeck) or similar low-level libraries, but this is untested.
- The Elgato Stream Deck MK.2 remains the best-supported option for Linux (see [prior-art.md](prior-art.md)).
