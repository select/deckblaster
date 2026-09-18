#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["cairosvg"]
# ///
"""Generate the Zoom "share screen" button icons.

The meeting-control icons (mic/video/hand/leave) are hand-made; the share
button previously used a blue filled icon from a different set. These are
generated from Material Design Icons (MDI) so they match the rest.

  share-start.png      monitor-share, white   — not sharing, press to start
  share-stop.png       monitor-off,   red     — currently sharing, press to stop
  no-meeting-share.png monitor-share, grey    — no active meeting

Icon source: Material Design Icons — https://icones.js.org/collection/mdi
             https://pictogrammers.com/library/mdi/ (Apache-2.0)
"""
import os

import cairosvg

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "assets")
SIZE = 120

# MDI path data (viewBox 0 0 24 24)
MDI = {
    "monitor-share": (
        "M23 4V16C23 17.11 22.11 18 21 18H15V16H21V4H3V16H9V18H3C1.9 18 1 17.11 1 16V4C1 "
        "2.89 1.89 2 3 2H21C22.1 2 23 2.89 23 4M13 13H16L12 9L8 13H11V20H8V22H16V20H13V13Z"
    ),
    "monitor-off": (
        "M14,18V20H16V22H8V20H10V18H3A2,2 0 0,1 1,16V4L0,3L1.41,1.58L22.16,22.34L20.75,23.75"
        "L15,18H14M3,16H13L3,6V16M21,2A2,2 0 0,1 23,4V16A2,2 0 0,1 21,18H20.66L18.66,16H21V4H"
        "6.66L4.66,2H21Z"
    ),
}

ICONS = {
    "share-start.png": ("monitor-share", "#FFFFFF"),
    "share-stop.png": ("monitor-off", "#FF4444"),
    "no-meeting-share.png": ("monitor-share", "#4B5563"),
}


def render(path_data, color, out_path):
    svg = (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">'
        f'<path fill="{color}" d="{path_data}"/></svg>'
    )
    cairosvg.svg2png(
        bytestring=svg.encode(),
        write_to=out_path,
        output_width=SIZE,
        output_height=SIZE,
    )


def main():
    os.makedirs(OUT, exist_ok=True)
    for name, (icon, color) in ICONS.items():
        render(MDI[icon], color, os.path.join(OUT, name))
        print("wrote", name)


if __name__ == "__main__":
    main()