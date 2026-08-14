package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LiveFile points at the session currently being captured on this machine, so
// that `sccap mark` and `sccap status` can find it without being told.
const LiveFile = "current-session.json"

// Live is the pointer record.
type Live struct {
	PID        int       `json:"pid"`
	BundleDir  string    `json:"bundle_dir"`
	BundleID   string    `json:"bundle_id"`
	StartedAt  time.Time `json:"started_at"`
	BeaconPort int       `json:"beacon_port"`
}

func livePath() string { return filepath.Join(DataDir(), LiveFile) }

// PublishLive records the running session.
func PublishLive(l Live) error {
	if err := os.MkdirAll(DataDir(), DirMode); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := livePath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), FileMode); err != nil {
		return err
	}
	return os.Rename(tmp, livePath())
}

// ReadLive returns the running session, if there is one.
//
// A stale pointer left behind by a killed process is treated as absent rather
// than reported as an error: SIGKILL is an expected way for a capture to end,
// and it must not leave the next command confused.
func ReadLive() (*Live, error) {
	b, err := os.ReadFile(livePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l Live
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, nil
	}
	if !processAlive(l.PID) {
		return nil, nil
	}
	if _, err := os.Stat(l.BundleDir); err != nil {
		return nil, nil
	}
	return &l, nil
}

// ClearLive removes the pointer at clean shutdown.
func ClearLive() error {
	err := os.Remove(livePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Linux FindProcess always succeeds; signal 0 is the actual test.
	return p.Signal(syscall.Signal(0)) == nil
}

// ErrNoLiveSession is returned when a command needs a running capture.
var ErrNoLiveSession = fmt.Errorf("no capture is running on this machine")
