//go:build npcap

package capture

import (
	"fmt"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
)

// The Npcap backend, used on Windows and buildable anywhere libpcap headers
// exist. Selected with `-tags npcap`.
//
// This is the one part of the project that needs cgo, and it is gated behind a
// build tag for that reason: without the tag the binary still builds statically
// and every offline command — verify, decode, index, coverage — works on an
// archived session. Only live capture needs the tag.
//
// Npcap is a separate install and its licence does not permit redistribution,
// so it cannot be bundled. `sccap doctor` reports its absence with the download
// location rather than failing mysteriously at capture time.

type pcapSource struct {
	handle *pcap.Handle
}

func available() (bool, string) { return true, "" }

// openSource opens a live capture handle.
func openSource(iface string, snaplen int) (source, error) {
	size := int32(frameSize)
	if snaplen > 0 {
		size = int32(snaplen)
	}

	// Immediate mode, not a timeout: buffering frames to fill a batch would
	// delay the drop counter that tells a contributor their capture is going
	// wrong, and the traffic rates here are far too low to need batching.
	inactive, err := pcap.NewInactiveHandle(iface)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", iface, err)
	}
	defer inactive.CleanUp()

	if err := inactive.SetSnapLen(int(size)); err != nil {
		return nil, fmt.Errorf("snaplen: %w", err)
	}
	if err := inactive.SetPromisc(true); err != nil {
		return nil, fmt.Errorf("promiscuous mode: %w", err)
	}
	if err := inactive.SetTimeout(pollWait); err != nil {
		return nil, fmt.Errorf("timeout: %w", err)
	}
	if err := inactive.SetImmediateMode(true); err != nil {
		return nil, fmt.Errorf("immediate mode: %w", err)
	}
	if err := inactive.SetBufferSize(bufferBytes); err != nil {
		return nil, fmt.Errorf("buffer size: %w", err)
	}

	h, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("activate %s: %w (is Npcap installed, and are you running "+
			"as Administrator?)", iface, err)
	}
	// No BPF filter is ever set. Capture-time filtering is a permanent,
	// irreversible discard decision (Principle I).
	return &pcapSource{handle: h}, nil
}

func (s *pcapSource) read() ([]byte, gopacket.CaptureInfo, error) {
	data, ci, err := s.handle.ZeroCopyReadPacketData()
	if err == pcap.NextErrorTimeoutExpired {
		// A quiet link, not a failure. Report an empty read the way the
		// AF_PACKET path does so the caller's loop is identical.
		return nil, ci, err
	}
	return data, ci, err
}

func (s *pcapSource) stats() (packets, drops uint64, err error) {
	st, err := s.handle.Stats()
	if err != nil {
		return 0, 0, err
	}
	// PacketsIfDropped counts frames the interface itself discarded, which is
	// as much a hole in the archive as one the driver dropped.
	return uint64(st.PacketsReceived),
		uint64(st.PacketsDropped) + uint64(st.PacketsIfDropped), nil
}

func (s *pcapSource) close() {
	if s.handle != nil {
		s.handle.Close()
	}
}
