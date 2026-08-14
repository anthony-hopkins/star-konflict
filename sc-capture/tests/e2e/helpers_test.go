//go:build linux

// Package e2e exercises the real sccap binary against a real interface.
//
// Linux-only, and deliberately so. These tests read /proc to prove the capture
// process opens no TCP socket, call statfs for the disk-floor thresholds, and
// drive an AF_PACKET capture — none of which have a meaningful Windows
// equivalent. Tagging them is more honest than writing a version that compiles
// everywhere and asserts nothing on most of it.
//
// The platform-independent guarantees are covered by suites that DO run
// everywhere: tests/faultinjection (Principle II, byte-loss under every failure
// mode), tests/architecture (the import rules), and pkg/scproto (protocol
// parity against the archived reference).
//
// These tests run the built artifact rather than importing packages, because
// capture capability lives on the binary file. That also means they test what a
// contributor actually runs.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// binary locates the built sccap, building it if needed.
func binary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "out", "sccap")
	if _, err := os.Stat(bin); err != nil {
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/sccap")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building sccap: %v\n%s", err, out)
		}
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("cannot find go.mod")
	return ""
}

// requireCapture skips unless the binary can actually open a capture socket.
//
// Skipping is the honest outcome on a host without the capability: reporting a
// pass would be a lie, and failing would make the suite unusable anywhere the
// grant has not been made.
func requireCapture(t *testing.T) string {
	t.Helper()
	bin := binary(t)
	out, err := exec.Command(bin, "doctor").CombinedOutput()
	if err != nil {
		t.Skipf("sccap doctor reports this host cannot capture; skipping.\n%s", out)
	}
	return bin
}

// liveInterface returns the single live interface, or skips.
func liveInterface(t *testing.T) string {
	t.Helper()
	ifaces, err := os.ReadDir("/sys/class/net")
	if err != nil {
		t.Skip("cannot enumerate interfaces")
	}
	var live []string
	for _, i := range ifaces {
		name := i.Name()
		if name == "lo" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "carrier"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == "1" {
			live = append(live, name)
		}
	}
	if len(live) != 1 {
		t.Skipf("need exactly one live interface, found %v", live)
	}
	return live[0]
}

// generateTraffic makes a little outbound traffic so a capture is not empty.
func generateTraffic() {
	// Best effort — a DNS lookup crosses the wire on any connected host.
	for i := 0; i < 3; i++ {
		c, err := exec.Command("getent", "hosts", "example.com").Output()
		_, _ = c, err
		time.Sleep(150 * time.Millisecond)
	}
}

// findBundle returns the single bundle directory under parent.
func findBundle(t *testing.T, parent string) string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read %s: %v", parent, err)
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "SC_") {
			found = append(found, filepath.Join(parent, e.Name()))
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one bundle in %s, found %d", parent, len(found))
	}
	return found[0]
}
