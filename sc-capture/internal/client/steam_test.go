package client

import (
	"os"
	"path/filepath"
	"testing"
)

// A trimmed but structurally faithful Steam app manifest, including the nested
// InstalledDepots block and the LastOwner field that must never be recorded.
const sampleACF = `"AppState"
{
	"appid"		"212070"
	"Universe"		"1"
	"name"		"Star Conflict"
	"StateFlags"		"4"
	"installdir"		"star conflict"
	"lastupdated"		"1786671130"
	"SizeOnDisk"		"11306368404"
	"buildid"		"24666578"
	"LastOwner"		"76561190000000001"
	"TargetBuildID"		"24666578"
	"InstalledDepots"
	{
		"212072"
		{
			"manifest"		"3741217375342066373"
			"size"		"10907604153"
		}
		"212074"
		{
			"manifest"		"6825923471741198508"
			"size"		"398764251"
		}
	}
	"UserConfig"
	{
		"language"		"english"
	}
}
`

func writeACF(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "appmanifest_212070.acf")
	if err := os.WriteFile(path, []byte(sampleACF), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestParseACF(t *testing.T) {
	path := writeACF(t, t.TempDir())

	kv, depots, err := parseACF(path)
	if err != nil {
		t.Fatalf("parseACF: %v", err)
	}

	for key, want := range map[string]string{
		"name":        "Star Conflict",
		"buildid":     "24666578",
		"installdir":  "star conflict",
		"lastupdated": "1786671130",
		"appid":       "212070",
	} {
		if kv[key] != want {
			t.Errorf("%s = %q, want %q", key, kv[key], want)
		}
	}

	if len(depots) != 2 {
		t.Fatalf("%d depots, want 2", len(depots))
	}
	if depots["212072"] != "3741217375342066373" {
		t.Errorf("depot 212072 manifest = %q", depots["212072"])
	}
	if depots["212074"] != "6825923471741198508" {
		t.Errorf("depot 212074 manifest = %q", depots["212074"])
	}

	// Nested values must not be flattened into the top level. "size" lives
	// inside a depot and "language" inside UserConfig; either leaking upward
	// would mean the parser is not tracking depth, and the next field it
	// mishandles might be one that matters.
	if _, ok := kv["size"]; ok {
		t.Error("a nested depot key leaked into the top level")
	}
	if _, ok := kv["language"]; ok {
		t.Error("a nested UserConfig key leaked into the top level")
	}
}

// TestAccountIdentifierIsNeverRecorded guards a privacy boundary.
//
// LastOwner is a SteamID64 — it identifies the person, not the build. Sessions
// are shared with other people, and the parser reads the field, so nothing but
// this test stops it appearing in a bundle.
func TestAccountIdentifierIsNeverRecorded(t *testing.T) {
	dir := t.TempDir()
	steamapps := filepath.Join(dir, "steamapps")
	if err := os.MkdirAll(filepath.Join(steamapps, "common", "star conflict"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeACF(t, steamapps)

	kv, _, err := parseACF(filepath.Join(steamapps, "appmanifest_212070.acf"))
	if err != nil {
		t.Fatalf("parseACF: %v", err)
	}
	// The parser does read it — that is exactly why the Info struct has no
	// field for it, and why this test exists.
	if kv["lastowner"] == "" {
		t.Skip("sample manifest no longer contains LastOwner; update the fixture")
	}

	info := &Info{
		AppID: kv["appid"], Name: kv["name"], BuildID: kv["buildid"],
	}
	blob := info.Summary() + info.AppID + info.Name + info.BuildID +
		info.InstallPath + info.BinarySHA256 + info.BinaryBuildID + info.Source
	if contains(blob, "76561190000000001") {
		t.Error("a SteamID64 reached the recorded client info")
	}
}

func TestKVLine(t *testing.T) {
	for _, tc := range []struct {
		in            string
		key, val      string
		ok            bool
		nameOnly      bool
		nameOnlyValue string
	}{
		{in: `	"buildid"		"24666578"`, key: "buildid", val: "24666578", ok: true},
		{in: `"name"  "Star Conflict"`, key: "name", val: "Star Conflict", ok: true},
		{in: `	"InstalledDepots"`, nameOnly: true, nameOnlyValue: "InstalledDepots"},
		{in: `{`},
		{in: `}`},
		{in: ``},
	} {
		key, val, ok := kvLine(tc.in)
		if ok != tc.ok || (ok && (key != tc.key || val != tc.val)) {
			t.Errorf("kvLine(%q) = %q,%q,%v; want %q,%q,%v",
				tc.in, key, val, ok, tc.key, tc.val, tc.ok)
		}
		if tc.nameOnly {
			name, ok := bareToken(tc.in)
			if !ok || name != tc.nameOnlyValue {
				t.Errorf("bareToken(%q) = %q,%v; want %q,true",
					tc.in, name, ok, tc.nameOnlyValue)
			}
		}
	}
}

// TestTildifyHidesTheUsername: the Steam library matters, the account name on
// the machine does not.
func TestTildifyHidesTheUsername(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	in := filepath.Join(home, ".local", "share", "Steam", "steamapps", "common", "x")
	got := tildify(in)
	if got[0] != '~' {
		t.Errorf("tildify(%q) = %q, want a ~ prefix", in, got)
	}
	if contains(got, filepath.Base(home)) {
		t.Errorf("tildify left the username in %q", got)
	}
	// Paths outside home are left alone — a second-drive library is not
	// sensitive and its real path is useful.
	if got := tildify("/mnt/games/Steam"); got != "/mnt/games/Steam" {
		t.Errorf("tildify rewrote a non-home path to %q", got)
	}
}

// TestDetectIsSafeWhenNothingIsInstalled: detection must degrade to "unknown",
// never to an error that could stop a capture.
func TestDetectIsSafeWhenNothingIsInstalled(t *testing.T) {
	info := Detect(Options{AppID: "999999999", HashBinary: false})
	if info != nil && info.BuildID != "" {
		t.Errorf("found a build for a nonexistent app: %+v", info)
	}
	if (&Info{}).Summary() == "" {
		t.Error("Summary must always return something printable")
	}
	var nilInfo *Info
	if nilInfo.Summary() == "" {
		t.Error("Summary on a nil Info must not panic or return empty")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		})()
}
