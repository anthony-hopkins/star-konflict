//go:build !windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// secureDir enforces owner-only access on a session directory.
func secureDir(path string) error { return os.Chmod(path, DirMode) }

// inspectPermissions reports the directory mode and anything group- or
// world-accessible inside it.
func inspectPermissions(dir string) (summary string, loose []string, err error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return "", nil, err
	}
	mode := fi.Mode().Perm()
	summary = fmt.Sprintf("mode %04o", mode)
	if mode&0o077 != 0 {
		loose = append(loose, filepath.Base(dir)+"/")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return summary, loose, err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode().Perm()&0o077 != 0 {
			loose = append(loose, e.Name())
		}
	}
	return summary, loose, nil
}
