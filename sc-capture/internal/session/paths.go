package session

import (
	"os"
	"path/filepath"
)

// DataDir is where machine-wide state lives — currently just the cross-session
// coverage store. Follows the XDG base directory spec.
func DataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "sccap")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sccap")
	}
	return filepath.Join(home, ".local", "share", "sccap")
}

// CaptureDirName is the directory bundles land in by default, kept at the root
// of the enclosing repository so every session ends up in one place no matter
// which subdirectory the tool was started from.
const CaptureDirName = "packet-caps"

// DefaultCaptureDir resolves where bundles go when --out is not given:
// packet-caps/ at the root of the enclosing repository, found by walking up
// from the working directory to the first .git. Outside any repository — a
// contributor running a bare distributed binary — it falls back to
// packet-caps/ under the working directory.
func DefaultCaptureDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return CaptureDirName
	}
	for d := wd; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return filepath.Join(d, CaptureDirName)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return filepath.Join(wd, CaptureDirName)
		}
		d = parent
	}
}
