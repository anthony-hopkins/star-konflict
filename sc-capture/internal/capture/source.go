// Package capture reads frames off the wire.
//
// This is the evidence-producing path. Nothing here interprets a byte: no
// decoder, no framing, no protocol knowledge. That is not an oversight — it is
// the property that makes Principle II structural. A decoder cannot lose a byte
// it never sat in front of.
package capture

import (
	"fmt"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/afpacket"
)

// Ring buffer sizing. Generous relative to game traffic (single-digit Mbit/s),
// because the cost of a too-small ring is dropped frames, and a dropped frame
// is the one failure this tool exists to prevent.
const (
	blockSize = 8 << 20 // 8 MiB per block
	numBlocks = 8       // 64 MiB total per interface
	frameSize = 1 << 16
	pollWait  = 100 * time.Millisecond
)

// Source is one interface being captured.
type Source struct {
	Name    string
	IfaceID int // pcapng interface id assigned by the journal

	tp *afpacket.TPacket
}

// Open binds an AF_PACKET v3 ring to iface.
//
// snaplen 0 means capture the whole frame, which is the default and the only
// value that satisfies Principle I — anything else is a capture-time
// truncation decision.
func Open(iface string, snaplen int) (*Source, error) {
	opts := []any{
		afpacket.OptInterface(iface),
		afpacket.OptTPacketVersion(afpacket.TPacketVersion3),
		afpacket.OptBlockSize(blockSize),
		afpacket.OptNumBlocks(numBlocks),
		afpacket.OptPollTimeout(pollWait),
	}
	if snaplen > 0 {
		opts = append(opts, afpacket.OptFrameSize(snaplen))
	} else {
		opts = append(opts, afpacket.OptFrameSize(frameSize))
	}

	tp, err := afpacket.NewTPacket(opts...)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w (missing CAP_NET_RAW?)", iface, err)
	}
	return &Source{Name: iface, tp: tp}, nil
}

// Read returns the next frame. The returned slice aliases the ring buffer and
// is only valid until the next call — callers that retain it must copy.
func (s *Source) Read() ([]byte, gopacket.CaptureInfo, error) {
	return s.tp.ZeroCopyReadPacketData()
}

// Stats returns cumulative packets seen and packets dropped by the kernel.
//
// The drop counter is not diagnostics. It is the number that tells a
// contributor their capture is worthless, and it is what SC-002 and SC-003
// assert on. It is shown on screen for the whole session for that reason.
func (s *Source) Stats() (packets, drops uint64, err error) {
	v1, v3, err := s.tp.SocketStats()
	if err != nil {
		return 0, 0, err
	}
	// V3 stats are authoritative when the ring is V3; V1 is returned zeroed in
	// that case, so take whichever reported something.
	p := uint64(v1.Packets()) + uint64(v3.Packets())
	d := uint64(v1.Drops()) + uint64(v3.Drops())
	return p, d, nil
}

// Close releases the ring.
func (s *Source) Close() {
	if s.tp != nil {
		s.tp.Close()
	}
}
