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

// offloadUnknownDetail is what the shared check says when nothing could be
// read. See offloadSummary for why nothing can be.
const offloadUnknownDetail = "cannot be determined here — if LSO or RSC is on, " +
	"captured frame boundaries and timings will be the driver's reconstruction"

func offloadRemedy(iface string) string {
	return `Get-NetAdapterLso -Name "` + iface + `"; Get-NetAdapterRsc -Name "` + iface + `"` +
		"\n" + `         Disable-NetAdapterLso -Name "` + iface + `"; ` +
		`Disable-NetAdapterRsc -Name "` + iface + `"    (run as Administrator)`
}

// checkCapabilities reports whether this process can open a capture handle.
//
// Two separate requirements, and conflating them produces a confusing failure:
// the binary must be built with a capture backend, and the process must be
// elevated. Either one missing stops a capture, for entirely different reasons
// with entirely different remedies.
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
// The electrical link state is not reachable through the standard library, so
// this uses the practical test instead: an interface that is up and holds a
// routable unicast address is carrying traffic. That is the property the caller
// actually cares about — "can the game reach a server through this" — and on a
// machine with several virtual adapters it is the more useful of the two
// answers anyway.
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

// offloadSummary is deliberately not implemented.
//
// LSO, RSC and checksum offload live behind per-driver WMI properties with no
// stable names across vendors, and the `Get-NetAdapter*` cmdlets that do read
// them reliably are not something this tool will shell out to — a diagnostic
// that guesses is worse than one that admits it does not know.
//
// Returning nothing is not the same as staying quiet: reportOffloads turns an
// empty answer into an explicit "cannot be determined, here is how to look",
// so a contributor is told to check rather than told a wrong answer.
func offloadSummary(iface string) string { return "" }

// checkClock reports whether the system clock is disciplined.
//
// A warning rather than a failure: monotonic ordering within a session survives
// an undisciplined clock, and a wall-clock step is recorded as an anchor. What
// suffers is correlation between contributors — lining two recordings of the
// same match up against each other, which is exactly what SC-T3-01 is for.
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
