package faultinjection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sc-re/sc-capture/internal/decode"
	"github.com/sc-re/sc-capture/pkg/scproto"
)

// TestOfflineDecodeIsReproducible covers US5, FR-029, FR-030 and SC-007.
//
// This is the payoff for keeping raw bytes as evidence: long after the servers
// are gone, decoding an archived session must produce the same answer it
// produced at capture time, and must not touch the evidence while doing it.
// Nothing here contacts a network.
func TestOfflineDecodeIsReproducible(t *testing.T) {
	dir := t.TempDir()

	var seq uint32 = 200
	var segs []segmentSpec
	for i := 0; i < 6; i++ {
		msg := scproto.Build(scproto.CSCMDAsyncReq, uint16(i), 0,
			[]byte{0x00, byte(i), 0xaa})
		segs = append(segs, segmentSpec{seq: seq, payload: msg})
		seq += uint32(len(msg))
	}
	path := buildSession(t, dir, segs)
	before := hashFile(t, path)

	first, _, _ := replay(t, dir)
	second, _, _ := replay(t, dir)

	if hashFile(t, path) != before {
		t.Fatal("decoding modified the raw journal; the evidence must be read-only")
	}
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("record counts differ between runs: %d then %d", len(first), len(second))
	}

	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Error("two decodes of the same archive produced different results; " +
			"an archive that decodes differently each time cannot be trusted")
	}
	for _, r := range first {
		if r.Decode.DecoderVersion == "" {
			// Without a stamp there is no way to tell, years from now, whether
			// a difference is an improvement or a regression.
			t.Errorf("record %d carries no decoder version", r.Seq)
			break
		}
	}
}

// TestTruncatedIndexIsTolerated: a torn final line is the expected shape of an
// abrupt kill, and must not invalidate everything before it (research.md R3).
func TestTruncatedIndexIsTolerated(t *testing.T) {
	dir := t.TempDir()

	var lines []byte
	for i := 1; i <= 5; i++ {
		b, _ := json.Marshal(decode.Record{
			Seq: uint64(i), ConnID: "c001", Dir: "c2s", TWall: time.Now().UTC(),
			Frames: []decode.FrameRef{{Segment: "capture_00001.pcapng", Index: uint64(i)}},
		})
		lines = append(lines, b...)
		lines = append(lines, '\n')
	}
	// Chop the last line mid-JSON, exactly as SIGKILL would.
	truncated := lines[:len(lines)-25]

	path := filepath.Join(dir, "index.jsonl")
	if err := os.WriteFile(path, truncated, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	records := readIndexLines(t, path)
	if len(records) != 4 {
		t.Errorf("recovered %d records from a truncated index, want 4 — the torn "+
			"final line must be dropped and the rest kept", len(records))
	}
}

func readIndexLines(t *testing.T, path string) []decode.Record {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out []decode.Record
	start := 0
	for i := 0; i <= len(b); i++ {
		if i == len(b) || b[i] == '\n' {
			if i > start {
				var r decode.Record
				if err := json.Unmarshal(b[start:i], &r); err == nil {
					out = append(out, r)
				}
			}
			start = i + 1
		}
	}
	return out
}
