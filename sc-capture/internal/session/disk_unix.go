//go:build !windows

package session

import (
	"fmt"
	"syscall"
)

// freeBytes reports space available to this unprivileged user.
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	// Bavail, not Bfree: Bfree includes blocks reserved for root, which we
	// cannot actually use and which would make us optimistic at exactly the
	// wrong moment.
	// Both conversions are explicit: the kernel types differ per BSD/Linux
	// variant (one has these signed, another unsigned), and an implicit
	// mismatch would be a compile error on some platforms and silent
	// truncation risk on others.
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
