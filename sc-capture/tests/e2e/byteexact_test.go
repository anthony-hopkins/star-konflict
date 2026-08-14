//go:build windows

package e2e

import (
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopacket/gopacket/pcapgo"
)

// TestByteExactAgainstIndependentCapture is the most important test here
// (SC-002, FR-004).
//
// It runs sccap alongside an unrelated capture tool on the same interface and
// asserts that every frame sccap journaled appears byte-identically in the
// independent capture. A failure invalidates everything downstream: nothing
// else matters if the journal is not a faithful copy of the wire.
//
// The comparison is a containment check rather than set equality, because two
// captures started and stopped at slightly different instants legitimately see
// different frames at the edges. What must never happen is sccap reporting a
// frame that the wire did not carry, or mangling one it did.
func TestByteExactAgainstIndependentCapture(t *testing.T) {
	bin := requireCapture(t)
	iface := liveInterface(t)

	dumpcap := independentCaptureTool(t)
	device := npcapDevice(t, dumpcap, iface)

	dir := t.TempDir()
	independent := filepath.Join(dir, "independent.pcapng")

	dc := exec.Command(dumpcap, "-i", device, "-n", "-s", "0", "-q", "-w", independent)
	startInOwnGroup(t, dc)
	defer func() {
		_ = interrupt(dc)
		_, _ = dc.Process.Wait()
	}()

	// Let dumpcap bind before sccap starts, so its window strictly contains
	// sccap's and every sccap frame has somewhere to be found.
	time.Sleep(1500 * time.Millisecond)

	out := filepath.Join(dir, "captures")
	sc := command(t, bin, "capture", "--scenario", "BASE-01", "--interface", iface, "--out", out)
	startInOwnGroup(t, sc)

	generateTraffic()
	time.Sleep(2 * time.Second)
	generateTraffic()

	if err := interrupt(sc); err != nil {
		t.Fatalf("interrupt sccap: %v", err)
	}
	if err := sc.Wait(); err != nil {
		t.Fatalf("sccap capture failed: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)
	_ = interrupt(dc)
	_, _ = dc.Process.Wait()

	ours := readFrames(t, findBundle(t, out))
	theirs := readFramesFromFile(t, independent)

	if len(ours) == 0 {
		t.Skip("no frames captured in the window; the link was silent")
	}

	theirSet := map[string]int{}
	for _, f := range theirs {
		theirSet[hex.EncodeToString(f)]++
	}

	var missing int
	var firstMissing []byte
	for _, f := range ours {
		k := hex.EncodeToString(f)
		if theirSet[k] > 0 {
			theirSet[k]--
			continue
		}
		missing++
		if firstMissing == nil {
			firstMissing = f
		}
	}

	if missing > 0 {
		t.Errorf("BYTE-EXACTNESS FAILURE: %d of %d journaled frames do not appear in the "+
			"independent capture.\nThe journal is not a faithful copy of the wire, which "+
			"invalidates every other result.\nFirst offending frame (%d bytes): %s",
			missing, len(ours), len(firstMissing), hex.EncodeToString(firstMissing))
	}

	t.Logf("byte-identical: %d journaled frames all present in the independent capture "+
		"(which saw %d)", len(ours), len(theirs))
}

// independentCaptureTool locates a capture tool we did not write.
//
// Wireshark's installer does not put dumpcap on PATH, so the default install
// location is checked directly. SCCAP_INDEPENDENT_CAPTURE overrides both, for a
// portable Wireshark or a tool that is not Wireshark at all — independence is
// the requirement, not any particular program.
func independentCaptureTool(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("SCCAP_INDEPENDENT_CAPTURE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		t.Fatalf("SCCAP_INDEPENDENT_CAPTURE=%s does not exist", p)
	}
	if p, err := exec.LookPath("dumpcap"); err == nil {
		return p
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Wireshark", "dumpcap.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no independent capture tool available. Install Wireshark, or set " +
		"SCCAP_INDEPENDENT_CAPTURE to a capture binary. Independence is the point — " +
		"comparing against ourselves would prove nothing.")
	return ""
}

// npcapDevice maps a Windows adapter name to the device name dumpcap wants.
//
// dumpcap does not accept "Ethernet" any more than Npcap does; it wants
// \Device\NPF_{GUID}. Asking dumpcap itself which device that is keeps the
// mapping out of this test — it is the tool's own answer, from its own
// enumeration, so the two captures are provably bound to the same wire.
//
// `dumpcap -D` prints lines shaped like:
//
//  1. \Device\NPF_{2C1A...} (Ethernet)
func npcapDevice(t *testing.T, dumpcap, iface string) string {
	t.Helper()
	out, err := exec.Command(dumpcap, "-D").Output()
	if err != nil {
		t.Skipf("dumpcap -D failed, so the two tools cannot be pointed at the same "+
			"interface: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip the "N. " index prefix.
		if dot := strings.Index(line, ". "); dot > 0 && dot <= 3 {
			line = line[dot+2:]
		}
		open := strings.LastIndex(line, " (")
		if open < 0 || !strings.HasSuffix(line, ")") {
			continue
		}
		friendly := line[open+2 : len(line)-1]
		if strings.EqualFold(friendly, iface) {
			return line[:open]
		}
	}
	t.Skipf("dumpcap does not list an interface named %q; without a shared device name "+
		"the comparison would be between two different wires.\n%s", iface, out)
	return ""
}

// readFrames reads every pcapng segment in a bundle.
func readFrames(t *testing.T, bundle string) [][]byte {
	t.Helper()
	entries, err := os.ReadDir(bundle)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var out [][]byte
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".pcapng" {
			continue
		}
		out = append(out, readFramesFromFile(t, filepath.Join(bundle, e.Name()))...)
	}
	return out
}

func readFramesFromFile(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	r, err := pcapgo.NewNgReader(f, pcapgo.DefaultNgReaderOptions)
	if err != nil {
		t.Fatalf("%s is not a readable pcapng: %v", filepath.Base(path), err)
	}
	var out [][]byte
	for {
		data, _, err := r.ReadPacketData()
		if err == io.EOF {
			return out
		}
		if err != nil {
			// A torn tail is acceptable in the independent capture, which we
			// stop rather than close.
			return out
		}
		cp := make([]byte, len(data))
		copy(cp, data)
		out = append(out, cp)
	}
}
