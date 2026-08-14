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
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

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

// Linux capability bit positions.
const (
	capNetAdmin = 12
	capNetRaw   = 13
)

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

func checkCapabilities(r *Report) {
	eff, err := effectiveCaps()
	if err != nil {
		r.add("capabilities", Warn, "could not read /proc/self/status: "+err.Error(),
			"Check manually with: getcap $(command -v sccap)")
		return
	}
	haveRaw := eff&(1<<capNetRaw) != 0
	haveAdmin := eff&(1<<capNetAdmin) != 0

	switch {
	case haveRaw && haveAdmin:
		r.add("capabilities", Pass, "CAP_NET_RAW and CAP_NET_ADMIN present", "")
	case haveRaw:
		// NET_RAW alone is enough to capture; NET_ADMIN is needed for some
		// interface queries. Not fatal.
		r.add("capabilities", Warn, "CAP_NET_RAW present, CAP_NET_ADMIN missing",
			"sudo setcap cap_net_raw,cap_net_admin=eip "+selfPath())
	default:
		r.add("capabilities", Fail, "CAP_NET_RAW missing — cannot open a capture socket",
			"sudo setcap cap_net_raw,cap_net_admin=eip "+selfPath())
	}
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		return "$(command -v sccap)"
	}
	return p
}

func effectiveCaps() (uint64, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		hex := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		return strconv.ParseUint(hex, 16, 64)
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("CapEff not found")
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

func carrier(name string) bool {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "carrier"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == "1"
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
				r.add("interfaces", Fail, want+" is down", "sudo ip link set "+want+" up")
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

func reportOffloads(r *Report, i InterfaceInfo) {
	if i.Offloads == "" {
		return
	}
	// Offloads make the NIC hand up reassembled super-frames that never
	// existed on the wire. The capture is still complete, but frame
	// boundaries and timing are the driver's fiction rather than the
	// network's.
	r.add("offloads ("+i.Name+")", Warn,
		"enabled: "+i.Offloads+" — captured frame boundaries will be synthetic",
		"sudo ethtool -K "+i.Name+" tso off gso off gro off lro off")
}

// ethtool ioctl plumbing. Only the simple value-get commands are used, which
// take a fixed two-word payload and avoid the variable-length GFEATURES dance.
const (
	siocEthtool  = 0x8946
	ethtoolGTSO  = 0x0000001e
	ethtoolGGSO  = 0x00000023
	ethtoolGGRO  = 0x0000002b
	ifnamsiz     = 16
	ifreqPadding = 16
)

type ethtoolValue struct {
	cmd  uint32
	data uint32
}

// ifreq is padded to the kernel's sizeof(struct ifreq) (40 bytes on 64-bit).
// Passing a shorter struct would have the kernel read past our allocation.
type ifreq struct {
	name [ifnamsiz]byte
	data uintptr
	_    [ifreqPadding]byte
}

func offloadSummary(iface string) string {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return ""
	}
	defer syscall.Close(fd)

	var on []string
	for _, f := range []struct {
		name string
		cmd  uint32
	}{
		{"tso", ethtoolGTSO},
		{"gso", ethtoolGGSO},
		{"gro", ethtoolGGRO},
	} {
		v, err := ethtoolGet(fd, iface, f.cmd)
		if err == nil && v != 0 {
			on = append(on, f.name)
		}
	}
	return strings.Join(on, ",")
}

func ethtoolGet(fd int, iface string, cmd uint32) (uint32, error) {
	if len(iface) >= ifnamsiz {
		return 0, fmt.Errorf("interface name too long")
	}
	val := ethtoolValue{cmd: cmd}
	var req ifreq
	copy(req.name[:], iface)
	req.data = uintptr(unsafe.Pointer(&val))

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), siocEthtool,
		uintptr(unsafe.Pointer(&req)))
	// Keep val alive across the syscall.
	defer func() { _ = val }()
	if errno != 0 {
		return 0, errno
	}
	return val.data, nil
}

// staUnsync is STA_UNSYNC from <sys/timex.h>: the kernel clock is not
// disciplined by any time source.
const staUnsync = 0x0040

func checkClock(r *Report) {
	var tx syscall.Timex
	state, err := syscall.Adjtimex(&tx)
	if err != nil {
		r.add("clock", Warn, "adjtimex unavailable: "+err.Error(),
			"Check with: timedatectl status")
		return
	}
	if tx.Status&staUnsync != 0 || state == 5 /* TIME_ERROR */ {
		// Not fatal: monotonic ordering within a session survives regardless,
		// and a step is recorded as a clock anchor. But cross-contributor
		// correlation depends on wall-clock accuracy.
		r.add("clock", Warn, "system clock is not synchronised to a time source",
			"sudo timedatectl set-ntp true   # or install chrony")
		return
	}
	r.add("clock", Pass, "system clock is disciplined", "")
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
