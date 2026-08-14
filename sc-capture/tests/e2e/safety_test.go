package e2e

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPassiveIsTheDefault covers FR-013 and Principle IV.
//
// Not "can be configured to be passive" — passive with no flags, without the
// contributor knowing to ask. Defaults are the only setting most people ever
// use, so the default is the policy.
func TestPassiveIsTheDefault(t *testing.T) {
	bundle := captureBriefly(t, nil)
	meta := readMeta(t, bundle)

	if meta.Mode != "passive" {
		t.Errorf("default mode = %q, want \"passive\"", meta.Mode)
	}
	if len(meta.Rewrites) != 0 {
		t.Errorf("passive mode performed %d rewrites; it must perform none", len(meta.Rewrites))
	}
}

// TestBundlePermissionsAreOwnerOnly covers FR-031.
//
// Checked on a real bundle rather than by reading the constant, because the
// value that matters is the mode on disk after umask and every intervening
// syscall have had their say.
func TestBundlePermissionsAreOwnerOnly(t *testing.T) {
	bundle := captureBriefly(t, nil)

	fi, err := os.Stat(bundle)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("bundle directory mode = %04o, want 0700 — a session may contain "+
			"login credentials in the clear", perm)
	}

	entries, err := os.ReadDir(bundle)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("bundle is empty")
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", e.Name(), err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s mode = %04o, want owner-only", e.Name(), perm)
		}
	}
}

// TestSessionIsMarkedSensitive covers FR-031: the contributor should not have
// to know that a capture is dangerous in order to be told.
func TestSessionIsMarkedSensitive(t *testing.T) {
	bundle := captureBriefly(t, nil)
	meta := readMeta(t, bundle)
	if !meta.Sensitive {
		t.Error("session is not marked sensitive")
	}

	raw, err := os.ReadFile(filepath.Join(bundle, "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	if !strings.Contains(string(raw), "sensitivity_reason") {
		t.Error("session.json states no reason for the sensitivity marking; " +
			"a bare flag tells a contributor nothing")
	}
}

// TestNoEgress covers FR-032 and SC-009.
//
// Rather than watching the wire, this inspects the capture process's own
// sockets while it runs and asserts it opened no TCP connection at all. That is
// the stronger claim: the binary contains no submission, telemetry or
// update-check path, so there is nothing that could fire. The only socket it
// legitimately holds is the UDP marker beacon on the local link.
func TestNoEgress(t *testing.T) {
	bin := requireCapture(t)
	iface := liveInterface(t)

	dir := t.TempDir()
	out := filepath.Join(dir, "captures")
	cmd := exec.Command(bin, "capture", "--scenario", "BASE-01", "--interface", iface, "--out", out)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		_ = cmd.Wait()
	}()

	pid := cmd.Process.Pid
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if conns := tcpConnections(t, pid); len(conns) > 0 {
			t.Errorf("the capture process opened TCP connections: %v\n"+
				"No captured data may leave the machine without an explicit action, and "+
				"this binary is supposed to contain no egress path at all.", conns)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// tcpConnections lists remote endpoints this pid has TCP sockets to.
func tcpConnections(t *testing.T, pid int) []string {
	t.Helper()

	inodes := map[string]bool{}
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil // process gone, or not readable — not a failure signal
	}
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(link, "socket:[") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	if len(inodes) == 0 {
		return nil
	}

	var found []string
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(table)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 {
				continue
			}
			// fields[3] is the connection state; 0A is LISTEN, 01 ESTABLISHED.
			if !inodes[fields[9]] {
				continue
			}
			if fields[3] == "0A" {
				continue // a listening socket is not egress
			}
			found = append(found, fields[2])
		}
		f.Close()
	}
	return found
}

// captureBriefly runs a short capture and returns the bundle directory.
func captureBriefly(t *testing.T, extraArgs []string) string {
	t.Helper()
	bin := requireCapture(t)
	iface := liveInterface(t)

	dir := t.TempDir()
	out := filepath.Join(dir, "captures")
	args := append([]string{"capture", "--scenario", "BASE-01",
		"--interface", iface, "--out", out}, extraArgs...)

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start capture: %v", err)
	}
	generateTraffic()
	time.Sleep(1200 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	return findBundle(t, out)
}
