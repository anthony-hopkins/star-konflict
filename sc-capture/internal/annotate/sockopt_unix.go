//go:build !windows

package annotate

import "syscall"

// enableBroadcast allows the marker beacon to send to the broadcast address,
// which is how a stamp lands inline in the packet capture alongside the game's
// own traffic.
func enableBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
