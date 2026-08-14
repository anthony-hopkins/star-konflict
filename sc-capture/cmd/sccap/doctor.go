package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sc-re/sc-capture/internal/doctor"
	"github.com/sc-re/sc-capture/internal/exitcode"
	"github.com/sc-re/sc-capture/internal/session"
	"github.com/sc-re/sc-capture/internal/version"
)

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	iface := fs.String("interface", "", "restrict checks to this interface")
	watch := fs.Duration("watch", 0, "sample live traffic for this long and report which interfaces carry game traffic (e.g. 30s)")
	asJSON := fs.Bool("json", false, "emit machine-readable output on stdout")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	if *watch > 0 {
		return doctorWatch(*watch, *iface, *asJSON)
	}

	rep := doctor.Run(*iface, session.DataDir(), version.TablesRevision)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			errf("%v", err)
			return exitcode.Usage
		}
	} else {
		fmt.Fprintln(os.Stderr, "Host diagnosis")
		fmt.Fprintln(os.Stderr)
		for _, c := range rep.Checks {
			fmt.Fprintf(os.Stderr, "  [%-4s] %-22s %s\n", c.Status, c.Name, c.Detail)
			if c.Remedy != "" {
				fmt.Fprintf(os.Stderr, "           %s\n", c.Remedy)
			}
		}
		fmt.Fprintln(os.Stderr)
		if rep.Failed() {
			fmt.Fprintln(os.Stderr, "Capture is not possible until the FAIL items above are resolved.")
		} else {
			fmt.Fprintln(os.Stderr, "This machine can capture.")
			fmt.Fprintln(os.Stderr, "Next, with the game running: sccap doctor --watch 30s")
		}
	}

	if rep.Failed() {
		return exitcode.NoCapability
	}
	return exitcode.OK
}

func doctorWatch(d time.Duration, iface string, asJSON bool) int {
	if !asJSON {
		fmt.Fprintf(os.Stderr, "Sampling traffic for %s — play the game or sit in the hangar...\n\n", d)
	}

	res, err := doctor.Watch(d, iface)
	if err != nil {
		errf("%v", err)
		return exitcode.NoCapability
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			errf("%v", err)
			return exitcode.Usage
		}
		return exitcode.OK
	}

	var best string
	for _, r := range res {
		mark := " "
		if r.GamePackets > 0 && best == "" {
			best = r.Name
			mark = "*"
		}
		fmt.Fprintf(os.Stderr, " %s %-12s packets=%-8d game=%-6d", mark, r.Name, r.Packets, r.GamePackets)
		if len(r.Services) > 0 {
			fmt.Fprintf(os.Stderr, " services=")
			first := true
			for k, v := range r.Services {
				if !first {
					fmt.Fprint(os.Stderr, ",")
				}
				fmt.Fprintf(os.Stderr, "%s(%d)", k, v)
				first = false
			}
		}
		if r.Err != "" {
			fmt.Fprintf(os.Stderr, "  error=%s", r.Err)
		}
		fmt.Fprintln(os.Stderr)
		for peer, n := range r.UDPPeers {
			if n > 50 { // a busy remote UDP flow — possibly a match
				fmt.Fprintf(os.Stderr, "      busy UDP peer %s (%d packets)\n", peer, n)
			}
		}
	}

	fmt.Fprintln(os.Stderr)
	switch {
	case best != "":
		fmt.Fprintf(os.Stderr, "Capture on %s:\n  sccap capture --interface %s\n", best, best)
	default:
		fmt.Fprintln(os.Stderr, "No game traffic seen on any interface.")
		fmt.Fprintln(os.Stderr, "Either the game was not talking to a server during the sample,")
		fmt.Fprintln(os.Stderr, "or its traffic is on a path this host cannot see.")
	}
	return exitcode.OK
}
