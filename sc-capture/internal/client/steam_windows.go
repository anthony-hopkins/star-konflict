//go:build windows

package client

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// steamLibraries returns every Steam library root worth checking.
//
// The registry first, because it is where Steam records the truth, and a
// contributor who moved their install is exactly the person whose capture we
// least want to land without a build identity. The fixed paths after it cover
// a broken or hand-cleaned registry.
//
// Every root is then asked for its libraryfolders.vdf, because a second drive
// is the normal place for an eleven-gigabyte game to live and Steam does not
// record those anywhere else.
func steamLibraries() []string {
	seen := map[string]bool{}
	var out []string

	add := func(p string) {
		if p == "" {
			return
		}
		// Steam writes forward slashes into the registry.
		p = filepath.Clean(filepath.FromSlash(p))
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		if fi, err := os.Stat(filepath.Join(p, "steamapps")); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}

	roots := append(registrySteamPaths(), fixedSteamPaths()...)

	// Walk a copy: add() only appends roots that exist, and a library folder
	// discovered below is itself a root that can declare further libraries.
	for _, r := range roots {
		add(r)
	}
	for i := 0; i < len(out); i++ {
		for _, extra := range libraryFolders(filepath.Join(out[i], "steamapps", "libraryfolders.vdf")) {
			add(extra)
		}
	}
	return out
}

// registrySteamPaths reads Steam's own record of where it is installed.
func registrySteamPaths() []string {
	var out []string
	for _, src := range []struct {
		root registry.Key
		path string
		name string
	}{
		// The per-user key is the most reliable: it is rewritten every time
		// Steam starts.
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		k, err := registry.OpenKey(src.root, src.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue(src.name)
		k.Close()
		if err == nil && v != "" {
			out = append(out, v)
		}
	}
	return out
}

// fixedSteamPaths covers the default install locations.
//
// Steam is a 32-bit application, so its default home is the x86 Program Files
// directory even on a 64-bit machine.
func fixedSteamPaths() []string {
	var out []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "ProgramW6432"} {
		if base := os.Getenv(env); base != "" {
			out = append(out, filepath.Join(base, "Steam"))
		}
	}
	if sd := os.Getenv("SystemDrive"); sd != "" {
		out = append(out, filepath.Join(sd+`\`, "Steam"))
	}
	return out
}
