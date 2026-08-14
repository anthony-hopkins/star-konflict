//go:build windows

package session

import "golang.org/x/sys/windows"

// stillActive is STILL_ACTIVE from the Windows SDK: the exit code reported for
// a process that has not exited.
const stillActive = 259

// processAlive reports whether a pid is still running.
//
// The usual Unix idiom — os.FindProcess plus Signal(0) — does not work here:
// Windows has no signals, and FindProcess opens a handle that keeps succeeding
// for a pid that has already exited but whose handle is still open. Asking for
// the exit code is the reliable test.
//
// PROCESS_QUERY_LIMITED_INFORMATION rather than the full query right: it is
// the least privilege that answers the question, and it works against
// processes this user could not otherwise inspect.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
