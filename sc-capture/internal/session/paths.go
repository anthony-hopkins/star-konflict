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
