package session

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// Disk thresholds. Defaults chosen so that reaching the floor still leaves
// room to finish writing session.json and SHA256SUMS — a clean close needs
// space, so the floor is deliberately well above zero.
const (
	DefaultMinFree = 2 << 30   // 2 GiB — start warning
	DefaultFloor   = 512 << 20 // 512 MiB — stop capturing, close cleanly
)

// DiskState is the result of a free-space check.
type DiskState int

const (
	DiskOK DiskState = iota
	DiskWarn
	DiskFloor
)

func (s DiskState) String() string {
	switch s {
	case DiskWarn:
		return "warn"
	case DiskFloor:
		return "floor"
	default:
		return "ok"
	}
}

// DiskMonitor watches free space on the filesystem holding a session.
//
// Retention is unbounded by design: this never deletes or rotates a prior
// session to reclaim space (FR-036). Its only job is to see the wall coming
// early enough that the current session can be closed cleanly rather than
// truncated (FR-037).
type DiskMonitor struct {
	path    string
	minFree uint64
	floor   uint64
}

func NewDiskMonitor(path string, minFree, floor uint64) *DiskMonitor {
	if minFree == 0 {
		minFree = DefaultMinFree
	}
	if floor == 0 {
		floor = DefaultFloor
	}
	return &DiskMonitor{path: path, minFree: minFree, floor: floor}
}

// Free returns bytes available to this (unprivileged) user.
func (d *DiskMonitor) Free() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", d.path, err)
	}
	// Bavail, not Bfree: Bfree includes blocks reserved for root, which we
	// cannot actually use and which would make us optimistic at exactly the
	// wrong moment.
	return st.Bavail * uint64(st.Bsize), nil
}

// Check reports the current state and the free byte count.
func (d *DiskMonitor) Check() (DiskState, uint64, error) {
	free, err := d.Free()
	if err != nil {
		return DiskOK, 0, err
	}
	switch {
	case free <= d.floor:
		return DiskFloor, free, nil
	case free <= d.minFree:
		return DiskWarn, free, nil
	default:
		return DiskOK, free, nil
	}
}

// ParseSize accepts "2GiB", "512MiB", "1G", "1048576" and similar.
func ParseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := uint64(1)
	upper := strings.ToUpper(s)
	for _, suf := range []struct {
		s string
		m uint64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
		{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"T", 1 << 40},
	} {
		if strings.HasSuffix(upper, suf.s) {
			mult = suf.m
			s = s[:len(s)-len(suf.s)]
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("bad size %q: %w", s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative size %q", s)
	}
	return uint64(n * float64(mult)), nil
}

// HumanSize renders a byte count for a human reading a terminal.
func HumanSize(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTP"[exp])
}
