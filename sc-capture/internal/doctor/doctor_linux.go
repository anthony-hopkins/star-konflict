//go:build linux

package doctor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Linux capability bit positions.
const (
	capNetAdmin = 12
	capNetRaw   = 13
)

func ifaceUpRemedy(iface string) string { return "sudo ip link set " + iface + " up" }

func offloadRemedy(iface string) string {
	return "sudo ethtool -K " + iface + " tso off gso off gro off lro off"
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

func carrier(name string) bool {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "carrier"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == "1"
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
