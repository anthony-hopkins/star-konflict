package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestDiskFloorClosesCleanly covers FR-036, FR-037 and the disk-exhaustion edge
// case.
//
// When space runs out the tool has only two honest options, because Principle I
// forbids dropping traffic and FR-036 forbids deleting old sessions: stop, or
// corrupt. It must stop — and stop with enough headroom left to finish writing
// the metadata that makes the session readable.
//
// The thresholds are set above actual free space rather than mounting a small
// filesystem, so the test exercises the real decision path without touching
// host state.
func TestDiskFloorClosesCleanly(t *testing.T) {
	bin := requireCapture(t)
	iface := liveInterface(t)

	dir := t.TempDir()
	out := filepath.Join(dir, "captures")

	free := freeBytes(t, dir)
	if free == 0 {
		t.Skip("cannot determine free space")
	}
	// Put the floor above what is available, so the very first check trips it.
	floor := free + (1 << 30)
	minFree := floor + (1 << 30)

	cmd := exec.Command(bin, "capture",
		"--scenario", "BASE-01",
		"--interface", iface,
		"--out", out,
		"--min-free", itoaBytes(minFree),
		"--floor", itoaBytes(floor),
	)
	output, err := cmd.CombinedOutput()

	// Exit code 4 is the contract for "stopped at the disk floor".
	code := exitCode(err)
	if code != 4 {
		t.Errorf("exit code = %d, want 4 (disk floor)\n%s", code, output)
	}
	if !strings.Contains(string(output), "floor") {
		t.Errorf("output does not explain why capture stopped:\n%s", output)
	}

	bundle := findBundle(t, out)
	meta := readMeta(t, bundle)

	// The point of stopping early rather than at zero: there was room left to
	// close properly.
	if meta.Termination != "disk_floor" {
		t.Errorf("termination = %q, want \"disk_floor\"", meta.Termination)
	}
	if meta.UTCEnd == nil {
		t.Error("a disk-floor stop is a clean close and must record utc_end")
	}
	if _, err := os.Stat(filepath.Join(bundle, "SHA256SUMS")); err != nil {
		t.Error("a disk-floor stop must still write SHA256SUMS — it closed cleanly")
	}

	// And the resulting session must verify.
	vout, verr := exec.Command(bin, "verify", bundle).CombinedOutput()
	if verr != nil {
		t.Errorf("verify rejected a disk-floor session (exit %v)\n%s", verr, vout)
	}
}

// TestPriorSessionsAreNeverReclaimed covers FR-036: the tool must never delete
// or overwrite a previous session to make room.
func TestPriorSessionsAreNeverReclaimed(t *testing.T) {
	bin := requireCapture(t)
	iface := liveInterface(t)

	dir := t.TempDir()
	out := filepath.Join(dir, "captures")

	// A decoy that looks exactly like a prior session.
	prior := filepath.Join(out, "SC_20200101T000000Z__OLD__vol-local____000")
	if err := os.MkdirAll(prior, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	marker := filepath.Join(prior, "capture_00001_20200101000000.pcapng")
	if err := os.WriteFile(marker, []byte("prior session evidence"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	free := freeBytes(t, dir)
	floor := free + (1 << 30)
	cmd := exec.Command(bin, "capture", "--scenario", "BASE-01", "--interface", iface,
		"--out", out, "--min-free", itoaBytes(floor+(1<<30)), "--floor", itoaBytes(floor))
	_, _ = cmd.CombinedOutput()

	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the prior session was removed when space ran out: %v", err)
	}
	if string(b) != "prior session evidence" {
		t.Error("the prior session was modified when space ran out")
	}
}

func freeBytes(t *testing.T, path string) uint64 {
	t.Helper()
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

func itoaBytes(n uint64) string {
	// Plain byte counts avoid any rounding surprise in the size parser.
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{digits[n%10]}, buf...)
		n /= 10
	}
	return string(buf)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if ok := asExitError(err, &ee); ok {
		return ee.ExitCode()
	}
	return -1
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
