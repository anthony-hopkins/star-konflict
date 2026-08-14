//go:build linux

package e2e

import (
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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

	dir := t.TempDir()
	independent := filepath.Join(dir, "independent.pcapng")

	dc := exec.Command(dumpcap, "-i", iface, "-n", "-s", "0", "-q", "-w", independent)
	if err := dc.Start(); err != nil {
		t.Skipf("cannot run dumpcap: %v", err)
	}
	defer func() {
		_ = dc.Process.Signal(syscall.SIGINT)
		_, _ = dc.Process.Wait()
	}()

	// Let dumpcap bind before sccap starts, so its window strictly contains
	// sccap's and every sccap frame has somewhere to be found.
	time.Sleep(1500 * time.Millisecond)

	out := filepath.Join(dir, "captures")
	sc := exec.Command(bin, "capture", "--scenario", "BASE-01", "--interface", iface, "--out", out)
	if err := sc.Start(); err != nil {
		t.Fatalf("start sccap: %v", err)
	}

	generateTraffic()
	time.Sleep(2 * time.Second)
	generateTraffic()

	if err := sc.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal sccap: %v", err)
	}
	if err := sc.Wait(); err != nil {
		t.Fatalf("sccap capture failed: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)
	_ = dc.Process.Signal(syscall.SIGINT)
	_, _ = dc.Process.Wait()

	ours := readFrames(t, filepath.Join(findBundle(t, out), ""))
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
// SCCAP_INDEPENDENT_CAPTURE lets a host point at a dumpcap that is present but
// not executable by this user — a common Ubuntu state, since dumpcap ships
// root:wireshark mode 0754 and grants execution by group membership only. The
// test needs a tool it did not write; it does not need that tool to be on PATH.
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
	t.Skip("no independent capture tool available. Install wireshark-common and join " +
		"the wireshark group, or set SCCAP_INDEPENDENT_CAPTURE to a capture binary. " +
		"Independence is the point — comparing against ourselves would prove nothing.")
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
			// kill rather than close.
			return out
		}
		cp := make([]byte, len(data))
		copy(cp, data)
		out = append(out, cp)
	}
}
