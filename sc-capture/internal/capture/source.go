// Package capture reads frames off the wire.
//
// This is the evidence-producing path. Nothing here interprets a byte: no
// decoder, no framing, no protocol knowledge. That is not an oversight — it is
// the property that makes Principle II structural. A decoder cannot lose a byte
// it never sat in front of.
//
// The acquisition mechanism is per platform (AF_PACKET on Linux, Npcap on
// Windows) but everything above this package sees one interface, so the
// journal, the record index and every guarantee they carry are identical
// wherever a session was recorded.
package capture

import (
	"time"

	"github.com/gopacket/gopacket"
)

// Ring/buffer sizing. Generous relative to game traffic (single-digit Mbit/s),
// because the cost of a too-small buffer is dropped frames, and a dropped frame
// is the one failure this tool exists to prevent.
const (
	bufferBytes = 64 << 20 // 64 MiB
	frameSize   = 1 << 16
	pollWait    = 100 * time.Millisecond
)

// source is what a platform must provide.
type source interface {
	// read returns the next frame. The slice may alias an internal buffer and
	// is only valid until the next call.
	read() ([]byte, gopacket.CaptureInfo, error)
	// stats returns cumulative packets seen and packets dropped by the kernel
	// or driver.
	stats() (packets, drops uint64, err error)
	close()
}

// Source is one interface being captured.
type Source struct {
	Name    string
	IfaceID int // pcapng interface id assigned by the journal

	impl source
}

// Open binds a capture handle to iface.
//
// snaplen 0 means capture the whole frame, which is the default and the only
// value that satisfies Principle I — anything else is a capture-time
// truncation decision.
func Open(iface string, snaplen int) (*Source, error) {
	impl, err := openSource(iface, snaplen)
	if err != nil {
		return nil, err
	}
	return &Source{Name: iface, impl: impl}, nil
}

// Read returns the next frame. The returned slice is only valid until the next
// call — callers that retain it must copy.
func (s *Source) Read() ([]byte, gopacket.CaptureInfo, error) { return s.impl.read() }

// Stats returns cumulative packets seen and packets dropped.
//
// The drop counter is not diagnostics. It is the number that tells a
// contributor their capture is worthless, and it is what SC-002 and SC-003
// assert on. It is shown on screen for the whole session for that reason.
func (s *Source) Stats() (packets, drops uint64, err error) { return s.impl.stats() }

// Close releases the handle.
func (s *Source) Close() {
	if s.impl != nil {
		s.impl.close()
	}
}

// Available reports whether this build can capture at all, and why not if it
// cannot. A build without a capture backend is still useful — verify, decode,
// index and coverage all work on an archived session — so this is a fact to
// report rather than a reason to refuse to start.
func Available() (bool, string) { return available() }
