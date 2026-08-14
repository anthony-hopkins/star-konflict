package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/exitcode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/session"
)

// runStatus reports on a running capture by reading its published state and
// sidecar. It deliberately does not attach to or signal the capture process:
// observing a capture must not be able to perturb it.
func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit machine-readable output on stdout")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	live, err := session.ReadLive()
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}
	if live == nil {
		if *asJSON {
			fmt.Println(`{"running":false}`)
			return exitcode.OK
		}
		fmt.Fprintln(os.Stderr, "No capture is running.")
		return exitcode.OK
	}

	meta, err := session.LoadMetadata(live.BundleDir)
	if err != nil {
		errf("reading session metadata: %v", err)
		return exitcode.Usage
	}

	var frames uint64
	for _, s := range meta.Segments {
		frames += s.Frames
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"running":          true,
			"bundle_id":        live.BundleID,
			"bundle_dir":       live.BundleDir,
			"pid":              live.PID,
			"elapsed_seconds":  int(time.Since(live.StartedAt).Seconds()),
			"frames":           frames,
			"packets_captured": meta.Host.PacketsCaptured,
			"packets_dropped":  meta.Host.PacketsDropped,
			"segments":         len(meta.Segments),
			"mode":             meta.Mode,
			"markers":          meta.Markers.Count,
		})
		return exitcode.OK
	}

	d := time.Since(live.StartedAt).Truncate(time.Second)
	fmt.Fprintf(os.Stderr, "Capturing (pid %d)\n", live.PID)
	fmt.Fprintf(os.Stderr, "  bundle    %s\n", live.BundleID)
	fmt.Fprintf(os.Stderr, "  elapsed   %s\n", d)
	fmt.Fprintf(os.Stderr, "  frames    %d in %d segment(s)\n", frames, len(meta.Segments))
	fmt.Fprintf(os.Stderr, "  drops     %d\n", meta.Host.PacketsDropped)
	fmt.Fprintf(os.Stderr, "  mode      %s\n", meta.Mode)
	fmt.Fprintf(os.Stderr, "  markers   %d\n", meta.Markers.Count)
	// Metadata is refreshed on the anchor interval, so these figures lag the
	// live capture by up to that long. Say so rather than let someone read a
	// stale frame count as a stalled capture.
	fmt.Fprintf(os.Stderr, "\n(figures refresh every %s)\n", session.AnchorInterval)
	return exitcode.OK
}
