//go:build windows

package session

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// freeBytes reports space available to this user on the volume holding path.
//
// GetDiskFreeSpaceEx returns three numbers; the first is the caller's quota-
// aware free space, which is the honest one. Total free space on the volume
// would over-report wherever disk quotas are in force, and over-reporting free
// space means running out mid-session — the one moment this number matters.
func freeBytes(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("path %s: %w", path, err)
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}
	return freeToCaller, nil
}
