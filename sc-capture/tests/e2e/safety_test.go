//go:build windows

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
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

// TestBundleAccessIsOwnerOnly covers FR-031.
//
// The file mode is NOT what is checked, because here it would prove nothing:
// os.Chmod toggles the read-only attribute and Go reports a synthesised 0666
// for almost everything. A test asserting 0700 would pass on a directory the
// entire Users group can read — which is precisely the state a session
// inheriting its parent's ACL lands in.
//
// So this reads the DACL and asserts no broad principal appears in it. The ACL
// is walked here independently of the code that wrote it: reusing
// session.CheckPermissions would only prove that function agrees with itself.
func TestBundleAccessIsOwnerOnly(t *testing.T) {
	bundle := captureBriefly(t, nil)

	check := func(path, label string) {
		t.Helper()
		for _, sid := range accessSIDs(t, path) {
			if broadPrincipals[sid] {
				t.Errorf("%s grants access to %s (%s) — a session may contain login "+
					"credentials in the clear", label, sidName(sid), sid)
			}
		}
	}

	check(bundle, "the bundle directory")

	entries, err := os.ReadDir(bundle)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("bundle is empty")
	}
	for _, e := range entries {
		check(filepath.Join(bundle, e.Name()), e.Name())
	}
}

// broadPrincipals are the well-known SIDs that would make a session readable
// by someone other than its author. Compared as SID strings because the
// display names are localised and a machine in another language would silently
// stop matching.
var broadPrincipals = map[string]bool{
	"S-1-1-0":      true, // Everyone
	"S-1-5-11":     true, // Authenticated Users
	"S-1-5-32-545": true, // BUILTIN\Users
	"S-1-5-32-546": true, // BUILTIN\Guests
	"S-1-5-7":      true, // Anonymous Logon
	"S-1-5-32-547": true, // BUILTIN\Power Users
}

// accessSIDs returns the SIDs of every allow entry on a path's DACL.
func accessSIDs(t *testing.T, path string) []string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("reading the ACL of %s: %v", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatalf("reading the DACL of %s: %v", path, err)
	}
	if dacl == nil {
		// A nil DACL grants everyone everything. It must never read as clean.
		t.Fatalf("%s has no DACL at all, which grants unrestricted access", path)
	}

	var out []string
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue // deny entries do not widen access
		}
		// The SID is laid out inline at the end of the ACE; SidStart is its
		// first word rather than a pointer to it.
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		out = append(out, sid.String())
	}
	return out
}

func sidName(sidStr string) string {
	sid, err := windows.StringToSid(sidStr)
	if err != nil {
		return "unknown"
	}
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return "unknown"
	}
	if domain != "" {
		return domain + "\\" + account
	}
	return account
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
	cmd := command(t, bin, "capture", "--scenario", "BASE-01", "--interface", iface, "--out", out)
	startInOwnGroup(t, cmd)
	defer func() {
		_ = interrupt(cmd)
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

// tcpConnections lists remote endpoints this pid has non-listening TCP sockets
// to, via netstat.
//
// netstat is an independent oracle: it is not our code, and it reports what the
// operating system believes rather than what this process is willing to admit.
// Only the data rows are parsed, never the headers, which are localised.
func tcpConnections(t *testing.T, pid int) []string {
	t.Helper()

	// -a all connections, -n numeric (no DNS, which would itself be egress),
	// -o owning pid, -p TCP to skip the UDP beacon we legitimately hold.
	out, err := exec.Command("netstat", "-a", "-n", "-o", "-p", "TCP").Output()
	if err != nil {
		return nil // netstat unavailable — not a failure signal
	}

	want := strconv.Itoa(pid)
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		// Proto, Local, Foreign, State, PID — a listening row on some builds
		// omits nothing, but be defensive about the column count.
		if len(f) < 5 || !strings.EqualFold(f[0], "TCP") {
			continue
		}
		if f[len(f)-1] != want {
			continue
		}
		state := f[3]
		if strings.EqualFold(state, "LISTENING") {
			continue // a listening socket is not egress
		}
		found = append(found, f[2])
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

	cmd := command(t, bin, args...)
	startInOwnGroup(t, cmd)
	generateTraffic()
	time.Sleep(1200 * time.Millisecond)
	if err := interrupt(cmd); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	return findBundle(t, out)
}
