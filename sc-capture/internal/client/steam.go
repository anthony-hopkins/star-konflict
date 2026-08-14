// Package client identifies the game client a session was captured against.
//
// A recording without its matching client build is frequently undecodable: the
// client contains the code that parses every server message, so it is both the
// key to interpreting a capture and the only way to recover a message's
// structure if that message was never recorded. Writing the build identity into
// every session removes an entire class of "which version was this?" that
// nobody can answer years later.
//
// Everything here is best effort. A client that cannot be found must never stop
// a capture — an unidentified recording is worth far more than no recording.
package client

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StarConflictAppID is the Steam application id.
const StarConflictAppID = "212070"

// Info is what we could learn about the installed client.
type Info struct {
	AppID       string            `json:"app_id,omitempty"`
	Name        string            `json:"name,omitempty"`
	BuildID     string            `json:"build_id,omitempty"`
	Depots      map[string]string `json:"depot_manifests,omitempty"`
	InstallPath string            `json:"install_path,omitempty"`
	LastUpdated *time.Time        `json:"last_updated,omitempty"`

	BinaryName    string `json:"binary_name,omitempty"`
	BinarySize    int64  `json:"binary_size,omitempty"`
	BinarySHA256  string `json:"binary_sha256,omitempty"`
	BinaryBuildID string `json:"binary_build_id,omitempty"`
	BinaryArch    string `json:"binary_arch,omitempty"`

	Platform string `json:"platform,omitempty"`
	Launcher string `json:"launcher,omitempty"`
	Runtime  string `json:"runtime,omitempty"`

	// Source records how this was found, so a reader can judge how much to
	// trust it — an autodetected install and a hand-supplied path are
	// different kinds of claim.
	Source string `json:"source,omitempty"`
}

// Options control detection.
type Options struct {
	AppID string
	// InstallDir overrides autodetection.
	InstallDir string
	// HashBinary controls whether the client executable is hashed. Hashing 20 MB
	// costs well under a second and buys an exact, unambiguous build identity —
	// a Steam build id can be reused, a hash cannot.
	HashBinary bool
}

// Detect finds the installed client. It returns nil if nothing was found;
// callers must treat that as "unknown", never as an error worth stopping for.
func Detect(o Options) *Info {
	if o.AppID == "" {
		o.AppID = StarConflictAppID
	}

	if o.InstallDir != "" {
		info := &Info{
			AppID:       o.AppID,
			InstallPath: o.InstallDir,
			Source:      "explicit --client-dir",
		}
		describeInstall(info, o.HashBinary)
		info.InstallPath = tildify(info.InstallPath)
		return info
	}

	for _, lib := range steamLibraries() {
		manifest := filepath.Join(lib, "steamapps", "appmanifest_"+o.AppID+".acf")
		kv, depots, err := parseACF(manifest)
		if err != nil {
			continue
		}

		info := &Info{
			AppID:    o.AppID,
			Name:     kv["name"],
			BuildID:  kv["buildid"],
			Depots:   depots,
			Launcher: "steam",
			Source:   "steam appmanifest",
		}
		// Deliberately NOT recorded: LastOwner, a SteamID64 that identifies the
		// account. Sessions are shared with other people, and the build
		// identity is what a reader needs — not who owned it.
		if ts, err := strconv.ParseInt(kv["lastupdated"], 10, 64); err == nil && ts > 0 {
			t := time.Unix(ts, 0).UTC()
			info.LastUpdated = &t
		}
		if dir := kv["installdir"]; dir != "" {
			info.InstallPath = filepath.Join(lib, "steamapps", "common", dir)
		}
		describeInstall(info, o.HashBinary)
		info.InstallPath = tildify(info.InstallPath)
		return info
	}
	return nil
}

// steamLibraries returns every Steam library root worth checking.
func steamLibraries() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	roots := []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "root"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
	}

	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		if seen[p] {
			return
		}
		if fi, err := os.Stat(filepath.Join(p, "steamapps")); err == nil && fi.IsDir() {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, r := range roots {
		add(r)
		// Additional libraries — a second drive is common, and the game is
		// as likely to be there as in the default location.
		for _, extra := range libraryFolders(filepath.Join(r, "steamapps", "libraryfolders.vdf")) {
			add(extra)
		}
	}
	return out
}

// libraryFolders pulls library paths out of libraryfolders.vdf.
func libraryFolders(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		key, val, ok := kvLine(line)
		if ok && key == "path" {
			out = append(out, val)
		}
	}
	return out
}

// parseACF reads a Valve KeyValues file, flattening the top level and pulling
// out the installed depot manifest ids.
//
// Depot manifests pin the exact content version, which is a stronger identity
// than the build id alone and — unlike LastOwner — identifies content rather
// than a person.
func parseACF(path string) (map[string]string, map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	kv := map[string]string{}
	depots := map[string]string{}

	var section []string
	var currentDepot string

	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "{":
			continue
		case t == "}":
			if len(section) > 0 {
				section = section[:len(section)-1]
			}
			if len(section) < 2 {
				currentDepot = ""
			}
			continue
		}

		if key, val, ok := kvLine(t); ok {
			switch {
			case len(section) == 1: // top level, inside "AppState"
				kv[strings.ToLower(key)] = val
			case len(section) == 3 && strings.EqualFold(section[1], "InstalledDepots") &&
				strings.EqualFold(key, "manifest"):
				depots[currentDepot] = val
			}
			continue
		}

		// A bare quoted token opens a new section on the following line.
		if name, ok := bareToken(t); ok {
			section = append(section, name)
			if len(section) == 3 && strings.EqualFold(section[1], "InstalledDepots") {
				currentDepot = name
			}
		}
	}

	if len(kv) == 0 {
		return nil, nil, fmt.Errorf("%s: no keys found", filepath.Base(path))
	}
	if len(depots) == 0 {
		depots = nil
	}
	return kv, depots, nil
}

// kvLine parses `"key"  "value"`.
func kvLine(s string) (key, val string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	rest := s[1:]
	i := strings.Index(rest, `"`)
	if i < 0 {
		return "", "", false
	}
	key = rest[:i]
	rest = strings.TrimSpace(rest[i+1:])
	if !strings.HasPrefix(rest, `"`) {
		return "", "", false
	}
	rest = rest[1:]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return "", "", false
	}
	return key, rest[:j], true
}

// bareToken parses a lone `"name"` that introduces a section.
func bareToken(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || !strings.HasPrefix(s, `"`) || !strings.HasSuffix(s, `"`) {
		return "", false
	}
	inner := s[1 : len(s)-1]
	if strings.Contains(inner, `"`) {
		return "", false
	}
	return inner, true
}

// describeInstall fills in what can be learned from the files on disk.
func describeInstall(info *Info, hash bool) {
	if info.InstallPath == "" {
		return
	}
	if _, err := os.Stat(info.InstallPath); err != nil {
		info.InstallPath = ""
		return
	}

	exe := findExecutable(info.InstallPath)
	if exe == "" {
		return
	}
	info.BinaryName = filepath.Base(exe)

	if fi, err := os.Stat(exe); err == nil {
		info.BinarySize = fi.Size()
	}
	if arch, buildID, err := readELF(exe); err == nil {
		info.BinaryArch = arch
		info.BinaryBuildID = buildID
		// A native ELF client means no Wine or Proton is in the picture, which
		// changes what a reader should expect of the capture — notably that
		// TLS key extraction is straightforward rather than a Wine-specific
		// exercise.
		info.Runtime = "native"
		info.Platform = "linux"
	}
	if hash {
		if sum, err := hashFile(exe); err == nil {
			info.BinarySHA256 = sum
		}
	}
}

// findExecutable picks the client binary: the largest executable regular file
// at the top of the install directory.
//
// A heuristic, but a stable one — game clients are an order of magnitude larger
// than their helper binaries, and the alternative is hardcoding a name that
// changes between platforms and builds.
func findExecutable(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type cand struct {
		path string
		size int64
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".so") || strings.Contains(name, ".so.") {
			continue
		}
		if info.Mode()&0o111 == 0 || info.Size() < 1<<20 {
			continue
		}
		cands = append(cands, cand{filepath.Join(dir, name), info.Size()})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].size > cands[j].size })
	return cands[0].path
}

// readELF returns the architecture and GNU build-id.
//
// The GNU build-id is the binary's canonical identity — the compiler stamped it
// and nothing else will collide with it. It is what lets somebody in 2031
// confirm they are looking at the same executable that produced a capture.
func readELF(path string) (arch, buildID string, err error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	arch = f.Machine.String()
	if f.Class == elf.ELFCLASS32 {
		arch += " (32-bit)"
	} else {
		arch += " (64-bit)"
	}

	sec := f.Section(".note.gnu.build-id")
	if sec == nil {
		return arch, "", nil
	}
	data, err := sec.Data()
	if err != nil || len(data) < 16 {
		return arch, "", nil
	}

	// ELF note: namesz, descsz, type, then name and desc, each padded to 4.
	nameSz := binary.LittleEndian.Uint32(data[0:4])
	descSz := binary.LittleEndian.Uint32(data[4:8])
	noteType := binary.LittleEndian.Uint32(data[8:12])
	const ntGNUBuildID = 3
	if noteType != ntGNUBuildID {
		return arch, "", nil
	}
	off := 12 + int((nameSz+3)&^uint32(3))
	if off+int(descSz) > len(data) {
		return arch, "", nil
	}
	return arch, hex.EncodeToString(data[off : off+int(descSz)]), nil
}

// tildify replaces the user's home directory with ~.
//
// Which Steam library the game lives in is worth recording — it explains a
// second-drive install or an unusual layout. The account name that happens to
// be in the path is not, and sessions get shared with other people.
func tildify(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Summary renders a one-line description for a terminal.
func (i *Info) Summary() string {
	if i == nil {
		return "client not identified"
	}
	parts := []string{}
	if i.Name != "" {
		parts = append(parts, i.Name)
	}
	if i.BuildID != "" {
		parts = append(parts, "build "+i.BuildID)
	}
	if i.BinaryBuildID != "" {
		short := i.BinaryBuildID
		if len(short) > 12 {
			short = short[:12]
		}
		parts = append(parts, "binary "+short)
	}
	if i.BinaryArch != "" {
		parts = append(parts, i.BinaryArch)
	}
	if len(parts) == 0 {
		return "client found but not identified"
	}
	return strings.Join(parts, ", ")
}
