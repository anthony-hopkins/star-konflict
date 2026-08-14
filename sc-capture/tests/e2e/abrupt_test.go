//go:build windows

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAbruptTerminationLeavesValidSession covers FR-006 and SC-008.
//
// Being terminated outright is not an error path to be tolerated — it is how a
// capture ends when a machine loses power mid-match, or when a contributor
// closes the console window, which Windows answers by killing the process after
// a couple of seconds with nothing this tool can intercept. That is precisely
// when the traffic was most interesting. A session killed this way must remain
// valid and verifiable up to the point of failure, every time.
func TestAbruptTerminationLeavesValidSession(t *testing.T) {
	bin := requireCapture(t)
	iface := liveInterface(t)

	trials := 5
	if testing.Short() {
		trials = 2
	}

	for i := 0; i < trials; i++ {
		dir := t.TempDir()
		out := filepath.Join(dir, "captures")

		cmd := command(t, bin, "capture", "--scenario", "BASE-01",
			"--interface", iface, "--out", out)
		if err := cmd.Start(); err != nil {
			t.Fatalf("trial %d: start: %v", i, err)
		}

		generateTraffic()
		time.Sleep(1500 * time.Millisecond)

		// TerminateProcess: no deferred close, no handler, no flush. The
		// process does not get to run another instruction.
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("trial %d: kill: %v", i, err)
		}
		_ = cmd.Wait()

		bundle := findBundle(t, out)

		// The sidecar must exist and describe the session, even though nothing
		// got the chance to write it at close.
		meta := readMeta(t, bundle)
		if meta.SchemaVersion == "" {
			t.Errorf("trial %d: session.json has no schema_version", i)
		}
		if meta.BundleID != filepath.Base(bundle) {
			t.Errorf("trial %d: bundle_id %q does not match directory %q",
				i, meta.BundleID, filepath.Base(bundle))
		}
		if meta.Termination != "interrupted" {
			t.Errorf("trial %d: termination = %q, want \"interrupted\"", i, meta.Termination)
		}
		if meta.UTCEnd != nil {
			t.Errorf("trial %d: a killed session must not claim an end time", i)
		}
		if !meta.Sensitive {
			t.Errorf("trial %d: session is not marked sensitive", i)
		}

		// At least one segment must be on disk with real frames in it.
		segs, _ := filepath.Glob(filepath.Join(bundle, "*.pcapng"))
		if len(segs) == 0 {
			t.Fatalf("trial %d: no pcapng segment survived", i)
		}
		frames := readFrames(t, bundle)
		if len(frames) == 0 {
			t.Errorf("trial %d: segment exists but contains no readable frames", i)
		}

		// And verify must accept it — as interrupted, not as failed.
		vcmd := command(t, bin, "verify", bundle)
		vout, verr := vcmd.CombinedOutput()
		if verr != nil {
			t.Errorf("trial %d: verify rejected an interrupted session (exit %v)\n%s",
				i, verr, vout)
		}
		if !strings.Contains(string(vout), "interrupted") {
			t.Errorf("trial %d: verify did not report the session as interrupted\n%s", i, vout)
		}
	}
}

type metaProbe struct {
	SchemaVersion string  `json:"schema_version"`
	BundleID      string  `json:"bundle_id"`
	Termination   string  `json:"termination"`
	UTCEnd        *string `json:"utc_end"`
	Sensitive     bool    `json:"sensitive"`
	Mode          string  `json:"mode"`
	Rewrites      []any   `json:"rewrites"`
}

func readMeta(t *testing.T, bundle string) metaProbe {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundle, "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var m metaProbe
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("session.json is not valid JSON after an abrupt kill: %v", err)
	}
	return m
}
