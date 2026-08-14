package session

import (
	"os"
	"path/filepath"
)

// DataDirEnv overrides where machine-wide state is kept.
//
// Chiefly for tests, which must not scribble on the contributor's real
// coverage store — the count of never-observed elements is cumulative across
// every session ever recorded on the machine, and a test run that polluted it
// would corrupt the one number this project is keeping score with.
const DataDirEnv = "SCCAP_DATA_DIR"

// DataDir is where machine-wide state lives — currently just the cross-session
// coverage store and the pointer to a running capture.
//
// LOCALAPPDATA, not APPDATA: this is per-machine state that should never
// follow a user onto another machine through a roaming profile. A coverage
// store that roamed would claim elements were observed on a machine that never
// recorded them.
func DataDir() string {
	if d := os.Getenv(DataDirEnv); d != "" {
		return filepath.Join(d, "sccap")
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "sccap")
	}
	// UserCacheDir resolves LOCALAPPDATA too, and falls back to the profile
	// directory when the environment has been stripped — a service context, or
	// a shell started without the usual variables.
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "sccap")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "AppData", "Local", "sccap")
	}
	return filepath.Join(os.TempDir(), "sccap")
}
