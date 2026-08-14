//go:build !windows

package session

import (
	"os"
	"syscall"
)

// processAlive reports whether a pid is still running.
//
// A stale pointer left by a killed capture must read as "no session running"
// rather than as an error: SIGKILL is an expected way for a capture to end, and
// it must not leave the next command confused.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix FindProcess always succeeds; signal 0 is the actual test.
	return p.Signal(syscall.Signal(0)) == nil
}
