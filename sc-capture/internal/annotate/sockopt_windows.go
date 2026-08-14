//go:build windows

package annotate

import "syscall"

// enableBroadcast allows the marker beacon to send to the broadcast address,
// which is how a stamp lands inline in the packet capture alongside the game's
// own traffic.
//
// Split out from marker.go only because the socket is a Handle here rather
// than the int every other platform's sockets API uses.
func enableBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
