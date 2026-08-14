package decode

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gopacket/gopacket/pcapgo"
	"github.com/sc-re/sc-capture/internal/flow"
)

// Replay re-runs decoding over an archived bundle's raw journal.
//
// This is the operation that makes the archive outlive its tooling. It needs no
// server, no network and nothing but the pcapng segments, and it never writes
// to them — so an improved decoder can be applied to a session captured years
// earlier and the evidence is byte-for-byte what it always was (FR-029, FR-030).
func Replay(bundleDir string, start time.Time, emit Emit) (*flow.Table, Stats, error) {
	segments, err := Segments(bundleDir)
	if err != nil {
		return nil, Stats{}, err
	}
	if len(segments) == 0 {
		return nil, Stats{}, fmt.Errorf("no pcapng segments in %s", bundleDir)
	}

	table := flow.NewTable()
	p := New(table, emit)

	for _, name := range segments {
		if err := replaySegment(p, bundleDir, name, start); err != nil {
			// A torn tail is the expected shape of an abrupt kill. Everything
			// before it is still good, so stop reading this segment and carry
			// on rather than discarding the session.
			if !strings.Contains(err.Error(), "unexpected EOF") && err != io.EOF {
				return table, p.Stats(), fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return table, p.Stats(), nil
}

func replaySegment(p *Pipeline, dir, name string, start time.Time) error {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()

	r, err := pcapgo.NewNgReader(f, pcapgo.DefaultNgReaderOptions)
	if err != nil {
		return fmt.Errorf("not a readable pcapng: %w", err)
	}

	var idx uint64
	for {
		data, ci, err := r.ReadPacketData()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		mono := int64(0)
		if !start.IsZero() {
			mono = int64(ci.Timestamp.Sub(start))
		}
		p.Feed(FrameRef{Segment: name, Index: idx}, data, ci.Timestamp, mono)
		idx++
	}
}

// Segments lists a bundle's pcapng files in capture order.
func Segments(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pcapng") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
