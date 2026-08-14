//go:build windows

// Package e2e exercises the real sccap binary against a real interface.
//
// These tests run the built artifact rather than importing packages, because
// what they are testing is what a contributor actually runs: an elevated
// process holding an Npcap handle, stopped with Ctrl+C, leaving a bundle on
// disk. None of that is reachable from inside the process under test.
//
// They need three things a build machine usually lacks — Npcap, elevation, and
// a live interface — so they skip rather than fail when any is missing.
// Skipping is the honest outcome: a pass would be a lie, and a failure would
// make the suite unusable everywhere the prerequisites have not been met.
//
// The platform-independent guarantees are covered by suites that run anywhere:
// tests/faultinjection (Principle II, byte loss under every failure mode),
// tests/architecture (the import rules), and pkg/scproto (protocol parity
// against the archived reference).
package e2e

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// binary locates the built sccap.exe, building it if needed.
//
// Built with `-tags npcap`, which is the configuration a capturing contributor
// runs. The offline build would let every test here pass its build step and
// then skip at `doctor`, reporting a green suite that exercised nothing — so a
// failure to produce a capture-capable binary is surfaced as a skip naming the
// reason instead.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(root, "out", "sccap.exe")
		cmd := exec.Command("go", "build", "-tags", "npcap", "-o", bin, "./cmd/sccap")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building sccap with -tags npcap: %w\n%s", err, out)
			return
		}
		builtPath = bin
	})
	if buildErr != nil {
		t.Skipf("no capture-capable binary available; install Npcap and its SDK.\n%v", buildErr)
	}
	return builtPath
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("cannot find go.mod above the test directory")
}

// requireCapture skips unless this host can actually capture.
//
// `doctor` is the authority rather than a check written here, so that "doctor
// says this machine can capture" and "capture works on this machine" can never
// disagree — the same question a contributor is told to ask before a session.
func requireCapture(t *testing.T) string {
	t.Helper()
	bin := binary(t)
	out, err := command(t, bin, "doctor").CombinedOutput()
	if err != nil {
		t.Skipf("sccap doctor reports this host cannot capture; skipping.\n"+
			"Capture needs Npcap installed and an elevated terminal.\n%s", out)
	}
	return bin
}

// command builds a command with machine-wide state redirected into the test's
// own directory.
//
// Without this, every run would fold its results into the contributor's real
// coverage store. That store is the cumulative count of protocol elements never
// observed across every session ever recorded on the machine — the one number
// this project keeps score with — and a test marking elements as seen would
// corrupt it silently and permanently.
func command(t *testing.T, bin string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "SCCAP_DATA_DIR="+t.TempDir())
	return cmd
}

// startInOwnGroup starts a process in a new console process group.
//
// This is what makes a clean stop testable. Windows has no signals: the only
// way to ask a console process to shut down the way Ctrl+C does is
// GenerateConsoleCtrlEvent, and that addresses a process GROUP. Without a
// group of its own the event would go to every process attached to this
// console — the test runner included.
func startInOwnGroup(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", filepath.Base(cmd.Path), err)
	}
}

// interrupt asks a process started by startInOwnGroup to stop cleanly.
//
// CTRL_BREAK rather than CTRL_C: a process created with
// CREATE_NEW_PROCESS_GROUP inherits Ctrl+C handling disabled, while Ctrl+Break
// is always delivered. The Go runtime surfaces both to the child as
// os.Interrupt, which is the same path a contributor's Ctrl+C takes.
func interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

// liveInterface returns the single live interface, or skips.
//
// Deliberately the same definition doctor uses — up, not loopback, holding a
// routable address — so the test and the tool agree on what "live" means.
func liveInterface(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	var live []string
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			live = append(live, i.Name)
			break
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
		_, _ = net.LookupHost("example.com")
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
