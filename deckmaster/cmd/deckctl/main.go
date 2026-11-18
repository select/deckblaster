package main

import (
	"fmt"
	"os"

	"github.com/muesli/deckmaster/internal/appplugins"
	"github.com/muesli/deckmaster/internal/calfetch"
	"github.com/muesli/deckmaster/internal/sysplugins"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: deckctl <command> [args...]

Commands:
  ha             Home Assistant controls and icons
  vdesktop       Virtual desktop renderer, poller, and switcher
  zoom           Zoom meeting controls and icons
  mainrow        Dynamic main-deck bottom row (Zoom / virtual desktops)
  recorder       Stream Deck screenshot recorder
  calc           Calculator widget engine
  highlight      Mouse pointer highlighter controls
  slots          Slot machine game engine
  polymarket     Polymarket prediction markets widget
  calendar-fetch Evolution Data Server calendar poller`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var code int
	switch cmd {
	case "ha":
		code = sysplugins.RunHA(args)
	case "vdesktop":
		code = sysplugins.RunVDesktop(args)
	case "zoom":
		code = sysplugins.RunZoom(args)
	case "mainrow":
		code = sysplugins.RunMainRow(args)
	case "recorder":
		code = sysplugins.RunRecorder(args)
	case "calc":
		code = appplugins.RunCalc(args)
	case "highlight":
		code = appplugins.RunHighlight(args)
	case "slots":
		code = appplugins.RunSlots(args)
	case "polymarket":
		code = appplugins.RunPolymarket(args)
	case "calendar-fetch":
		code = calfetch.RunCalFetch(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}

	os.Exit(code)
}
