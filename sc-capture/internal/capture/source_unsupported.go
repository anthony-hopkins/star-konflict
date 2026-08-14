//go:build !linux && !npcap

package capture

import (
	"errors"
	"runtime"

	"github.com/gopacket/gopacket"
)

// No capture backend is compiled into this build.
//
// This is a deliberate, useful configuration rather than a broken one. Live
// capture on Windows needs Npcap, which needs cgo and a separate install whose
// licence forbids redistribution. Everything else the tool does — verify,
// decode, index, coverage — reads an archived session and needs none of that,
// so a plain `go build` on Windows produces a binary that can analyse bundles
// even though it cannot record one.
//
// Build with `-tags npcap` for a capture-capable binary.

const buildHint = "this build has no capture backend. On Windows, install Npcap " +
	"(https://npcap.com) and rebuild with: go build -tags npcap ./cmd/sccap"

func available() (bool, string) {
	return false, "no capture backend compiled in for " + runtime.GOOS +
		"; " + buildHint
}

func openSource(iface string, snaplen int) (source, error) {
	return nil, errors.New(buildHint)
}

// Referenced so the shared constants do not read as unused in this build.
var (
	_ = bufferBytes
	_ = frameSize
	_ = pollWait
	_ gopacket.CaptureInfo
)
