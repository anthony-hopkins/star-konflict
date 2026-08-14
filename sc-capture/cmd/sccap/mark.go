package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sc-re/sc-capture/internal/annotate"
	"github.com/sc-re/sc-capture/internal/exitcode"
	"github.com/sc-re/sc-capture/internal/session"
)

func runMark(args []string) int {
	fs := flag.NewFlagSet("mark", flag.ContinueOnError)
	console := fs.Bool("console", false, "interactive: type a label and press Enter, repeatedly")
	bundleDir := fs.String("bundle", "", "bundle to mark (default: the running session)")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	dir := *bundleDir
	var startedAt time.Time
	if dir == "" {
		live, err := session.ReadLive()
		if err != nil {
			errf("%v", err)
			return exitcode.Usage
		}
		if live == nil {
			errf("no capture is running. Start one with 'sccap capture', " +
				"or name a bundle with --bundle.")
			return exitcode.Usage
		}
		dir = live.BundleDir
		startedAt = live.StartedAt
	} else if meta, err := session.LoadMetadata(dir); err == nil {
		startedAt = meta.UTCStart
	}

	beacon, err := annotate.OpenBeacon(dir, annotate.DefaultBeaconPort)
	if err != nil {
		errf("open marker log: %v", err)
		return exitcode.Usage
	}
	defer beacon.Close()

	stamp := func(label string) {
		now := time.Now().UTC()
		var mono int64
		if !startedAt.IsZero() {
			mono = int64(now.Sub(startedAt))
		}
		m, err := beacon.Stamp(annotate.KindEvent, label, now, mono)
		if err != nil {
			errf("stamp: %v", err)
			return
		}
		fmt.Fprintln(os.Stderr, m.Format())
	}

	if *console {
		fmt.Fprintln(os.Stderr, "Marker console. Type a label and press Enter. Ctrl+D to finish.")
		fmt.Fprintln(os.Stderr, "Mark before AND after each action you want to find later.")
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			label := strings.TrimSpace(sc.Text())
			if label == "" {
				continue
			}
			stamp(label)
		}
		return exitcode.OK
	}

	if fs.NArg() == 0 {
		errf(`usage: sccap mark "<label>"   |   sccap mark --console`)
		return exitcode.Usage
	}
	stamp(strings.Join(fs.Args(), " "))
	return exitcode.OK
}
