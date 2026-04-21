#!/bin/bash
# Switch to virtual desktop N
# Usage: vdesktop-switch.sh <desktop_num>
DISPLAY=:1 xdotool set_desktop "$1"
