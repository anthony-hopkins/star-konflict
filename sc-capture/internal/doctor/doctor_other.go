//go:build !linux && !windows

package doctor

import (
	"net"
	"runtime"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/capture"
)

// Fallback host diagnosis for platforms with no capture backend.
//
// The constitution requires that a missing backend degrade to a binary which
// still reads archives rather than one that refuses to run — so this exists to
// make the module build everywhere, and to say plainly what is and is not
// possible here rather than pretending to check things it cannot.
//
// Every offline command works on such a build: verify, decode, index and
// coverage all read an archived session and need no capture capability at all.

func ifaceUpRemedy(iface string) string {
	return "bring " + iface + " up using this platform's network tools"
}

func offloadRemedy(iface string) string {
	return "disable segmentation and receive offload on " + iface + " using this platform's tools"
}

func checkCapabilities(r *Report) {
	ok, why := capture.Available()
	if !ok {
		r.add("capture backend", Fail, why,
			"This build can still verify, decode, index and report coverage on archived sessions.")
		return
	}
	r.add("capture backend", Pass, "capture backend compiled in for "+runtime.GOOS, "")
}

// carrier reports whether an interface holds a routable address, which is the
// portable approximation of "this link is carrying traffic".
func carrier(name string) bool {
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return false
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip := ipnet.IP
			if !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
				return true
			}
		}
	}
	return false
}

// offloadSummary is not queried here. Reporting nothing is honest; guessing
// would be worse than silence in a diagnostic.
func offloadSummary(iface string) string { return "" }

func checkClock(r *Report) {
	r.add("clock", Warn, "clock discipline not checked on "+runtime.GOOS,
		"Confirm your system clock is synchronised before capturing")
}
