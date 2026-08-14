// Package doctor diagnoses whether this host can capture, and what is wrong
// if it cannot.
//
// It NEVER changes host state. Package installation, NIC offload control and
// network namespace setup are genuinely shell work and are out of scope
// (constitution, Additional Constraints); reimplementing them in Go would be a
// downgrade and shipping them as a script is forbidden. The obligation this
// package carries instead is stricter than the scripts it replaces: detect and
// report every condition that would silently compromise a capture, and name
// the exact remedy.
package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/client"
)

// Severity of a single check result.
type Severity int

const (
	Pass Severity = iota
	Warn
	Fail // capture is impossible or would be worthless
)

func (s Severity) String() string {
	switch s {
	case Warn:
		return "WARN"
	case Fail:
		return "FAIL"
	default:
		return "OK"
	}
}

// Check is one diagnosis.
type Check struct {
	Name     string   `json:"name"`
	Severity Severity `json:"-"`
	Status   string   `json:"status"`
	Detail   string   `json:"detail"`
	Remedy   string   `json:"remedy,omitempty"`
}

// Report is the full diagnosis.
type Report struct {
	Checks []Check `json:"checks"`
}

// Failed reports whether any check makes capture impossible.
func (r *Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Severity == Fail {
			return true
		}
	}
	return false
}

func (r *Report) add(name string, sev Severity, detail, remedy string) {
	r.Checks = append(r.Checks, Check{
		Name: name, Severity: sev, Status: sev.String(), Detail: detail, Remedy: remedy,
	})
}

// Run performs every host check. iface may be empty to check all interfaces.
func Run(iface string, coverageDir string, tablesRevision string) *Report {
	r := &Report{}
	checkCapabilities(r)
	checkInterfaces(r, iface)
	checkClock(r)
	checkCoverageDir(r, coverageDir)
	checkClient(r)
	r.add("protocol tables", Pass,
		fmt.Sprintf("embedded element universe revision %s", tablesRevision), "")
	return r
}

// checkClient reports whether the game build can be identified.
//
// Warn, never fail: a recording nobody can tie to a client build is much
// harder to decode later, but it is still evidence and capturing it beats
// refusing to start.
func checkClient(r *Report) {
	// Skip the hash here — doctor should stay instant, and capture does the
	// full identification anyway.
	info := client.Detect(client.Options{HashBinary: false})
	if info == nil {
		r.add("game client", Warn,
			"no Steam app manifest found; sessions will not record which build produced them",
			"Pass --client-dir <install path> to sccap capture, or note the build by hand")
		return
	}
	detail := info.Summary()
	if info.InstallPath != "" {
		detail += " at " + info.InstallPath
	}
	r.add("game client", Pass, detail, "")
}

// InterfaceInfo describes one candidate capture interface.
type InterfaceInfo struct {
	Name     string `json:"name"`
	Up       bool   `json:"up"`
	Carrier  bool   `json:"carrier"`
	Loopback bool   `json:"loopback"`
	Offloads string `json:"offloads,omitempty"`
}

// Interfaces enumerates interfaces worth capturing on.
func Interfaces() ([]InterfaceInfo, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []InterfaceInfo
	for _, i := range ifs {
		info := InterfaceInfo{
			Name:     i.Name,
			Up:       i.Flags&net.FlagUp != 0,
			Loopback: i.Flags&net.FlagLoopback != 0,
			Carrier:  carrier(i.Name),
		}
		if !info.Loopback {
			info.Offloads = offloadSummary(i.Name)
		}
		out = append(out, info)
	}
	return out, nil
}

func checkInterfaces(r *Report, want string) {
	ifs, err := Interfaces()
	if err != nil {
		r.add("interfaces", Fail, "cannot enumerate interfaces: "+err.Error(), "")
		return
	}

	var live []InterfaceInfo
	for _, i := range ifs {
		if i.Up && i.Carrier && !i.Loopback {
			live = append(live, i)
		}
	}

	switch {
	case want != "":
		for _, i := range ifs {
			if i.Name != want {
				continue
			}
			if !i.Up {
				r.add("interfaces", Fail, want+" is down", ifaceUpRemedy(want))
				return
			}
			if !i.Carrier && !i.Loopback {
				r.add("interfaces", Fail, want+" has no carrier — cable unplugged?", "")
				return
			}
			r.add("interfaces", Pass, want+" is up with carrier", "")
			reportOffloads(r, i)
			return
		}
		r.add("interfaces", Fail, "no such interface: "+want, "")
	case len(live) == 0:
		r.add("interfaces", Fail, "no non-loopback interface is up with a carrier", "")
	case len(live) == 1:
		r.add("interfaces", Pass, "one live interface: "+live[0].Name, "")
		reportOffloads(r, live[0])
	default:
		names := make([]string, len(live))
		for i, l := range live {
			names[i] = l.Name
		}
		// Ambiguity here is the setup for the only silent, total failure mode
		// this tool has: capturing the wrong wire yields a clean-looking,
		// worthless session.
		r.add("interfaces", Warn,
			"several live interfaces: "+strings.Join(names, ", "),
			"Confirm which one carries game traffic: sccap doctor --watch 30s")
		for _, l := range live {
			reportOffloads(r, l)
		}
	}
}

// reportOffloads warns about segmentation and receive offload.
//
// Offloads make the NIC hand up reassembled super-frames that never existed on
// the wire. The capture is still complete — every byte is there — but frame
// boundaries and inter-packet timing become the driver's reconstruction rather
// than the network's. Anything measuring tick rate, keepalive cadence or
// retransmission is reading fiction.
//
// An interface whose offload state could not be read still gets a warning.
// "Could not determine" is not "off", and a contributor told nothing will
// reasonably assume the second.
func reportOffloads(r *Report, i InterfaceInfo) {
	detail := offloadUnknownDetail
	if i.Offloads != "" {
		detail = "enabled: " + i.Offloads + " — captured frame boundaries will be synthetic"
	}
	r.add("offloads ("+i.Name+")", Warn, detail, offloadRemedy(i.Name))
}

func checkCoverageDir(r *Report, dir string) {
	if dir == "" {
		r.add("coverage store", Warn, "no coverage directory resolved", "")
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.add("coverage store", Warn, "cannot create "+dir+": "+err.Error(), "")
		return
	}
	probe := filepath.Join(dir, ".write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		r.add("coverage store", Warn, dir+" is not writable: "+err.Error(), "")
		return
	}
	_ = os.Remove(probe)
	r.add("coverage store", Pass, dir+" is writable", "")
}
