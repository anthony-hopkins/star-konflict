//go:build linux && !npcap

package capture

import (
	"fmt"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/afpacket"
)

// AF_PACKET v3 ring sizing.
const (
	blockSize = 8 << 20 // 8 MiB per block
	numBlocks = bufferBytes / blockSize
)

type afpacketSource struct {
	tp *afpacket.TPacket
}

func available() (bool, string) { return true, "" }

// openSource binds an AF_PACKET v3 ring.
//
// Pure Go — no cgo, no libpcap — which is what keeps the Linux build a static
// single binary with no runtime prerequisites.
func openSource(iface string, snaplen int) (source, error) {
	size := frameSize
	if snaplen > 0 {
		size = snaplen
	}
	tp, err := afpacket.NewTPacket(
		afpacket.OptInterface(iface),
		afpacket.OptTPacketVersion(afpacket.TPacketVersion3),
		afpacket.OptBlockSize(blockSize),
		afpacket.OptNumBlocks(numBlocks),
		afpacket.OptPollTimeout(pollWait),
		afpacket.OptFrameSize(size),
	)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w (missing CAP_NET_RAW?)", iface, err)
	}
	return &afpacketSource{tp: tp}, nil
}

func (s *afpacketSource) read() ([]byte, gopacket.CaptureInfo, error) {
	return s.tp.ZeroCopyReadPacketData()
}

func (s *afpacketSource) stats() (packets, drops uint64, err error) {
	v1, v3, err := s.tp.SocketStats()
	if err != nil {
		return 0, 0, err
	}
	// V3 stats are authoritative when the ring is V3; V1 is returned zeroed in
	// that case, so take whichever reported something.
	return uint64(v1.Packets()) + uint64(v3.Packets()),
		uint64(v1.Drops()) + uint64(v3.Drops()), nil
}

func (s *afpacketSource) close() {
	if s.tp != nil {
		s.tp.Close()
	}
}
