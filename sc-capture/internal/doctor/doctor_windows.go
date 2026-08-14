//go:build windows

package doctor

import (
	"net"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/capture"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func ifaceUpRemedy(iface string) string {
	return `Enable-NetAdapter -Name "` + iface + `"    (run as Administrator)`
}

func offloadRemedy(iface string) string {
	return `Disable-NetAdapterLso -Name "` + iface + `"; ` +
		`Disable-NetAdapterRsc -Name "` + iface + `"    (run as Administrator)`
}

// checkCapabilities reports whether this process can open a capture handle.
//
// Two separate requirements on Windows, and conflating them produces a
// confusing failure: the binary must be built with a capture backend, and the
// process must be elevated. Either one missing stops a capture, for entirely
// different reasons.
func checkCapabilities(r *Report) {
	if ok, why := capture.Available(); !ok {
		r.add("capture backend", Fail, why,
			"Install Npcap from https://npcap.com, then rebuild: go build -tags npcap ./cmd/sccap")
		return
	}
	r.add("capture backend", Pass, "Npcap backend compiled in", "")

	if !isElevated() {
		// Npcap can be installed in "restricted" mode, where only
		// Administrators may capture. Elevation is the common case and the
		// first thing to try.
		r.add("privileges", Fail,
			"not running as Administrator — opening a capture handle will be refused",
			"Right-click your terminal and choose \"Run as administrator\", then re-run")
		return
	}
	r.add("privileges", Pass, "running elevated", "")
}

func isElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

// carrier reports whether an interface has a usable link.
//
// Windows exposes no direct equivalent of /sys/class/net/*/carrier through the
// standard library, so this uses the practical test: an interface that is up
// and holds a routable unicast address is carrying traffic. That is the
// property the caller actually cares about — "can the game reach a server
// through this" — rather than the electrical state of the cable.
func carrier(name string) bool {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return false
	}
	if iface.Flags&net.FlagUp == 0 {
		return false
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		return true
	}
	return false
}

// offloadSummary is not queried on Windows.
//
// The equivalent settings (LSO, RSC, checksum offload) live behind per-driver
// WMI properties with no stable names across vendors, so a query here would be
// unreliable in exactly the way a diagnostic must not be. Reporting nothing is
// honest; the shared check surfaces the manual command instead, and a
// contributor is told to look rather than told a wrong answer.
func offloadSummary(iface string) string { return "" }

// checkClock reports whether the system clock is disciplined.
//
// The Windows Time service is the equivalent of the NTP daemon the Linux path
// checks via adjtimex. As there, this is a warning rather than a failure:
// monotonic ordering within a session survives an undisciplined clock, and a
// wall-clock step is recorded as an anchor. It is cross-contributor
// correlation that suffers.
func checkClock(r *Report) {
	m, err := mgr.Connect()
	if err != nil {
		r.add("clock", Warn, "cannot query the service manager: "+err.Error(),
			"Check manually: w32tm /query /status")
		return
	}
	defer m.Disconnect()

	s, err := m.OpenService("W32Time")
	if err != nil {
		r.add("clock", Warn, "the Windows Time service is not installed",
			"w32tm /register && net start w32time    (run as Administrator)")
		return
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		r.add("clock", Warn, "cannot query the Windows Time service: "+err.Error(),
			"Check manually: w32tm /query /status")
		return
	}
	if status.State != windows.SERVICE_RUNNING {
		r.add("clock", Warn, "the Windows Time service is not running",
			"net start w32time    (run as Administrator)")
		return
	}
	r.add("clock", Pass, "the Windows Time service is running", "")
}
