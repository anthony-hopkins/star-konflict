package session

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Bundle directory and file modes, passed to every create call.
//
// These are NOT what protects a session here. os.Chmod on Windows toggles the
// read-only attribute and nothing else, so a mode is close to decorative: the
// real enforcement is the owner-only DACL that secureDir installs, and which
// files created inside the bundle inherit. The constants are kept because the
// standard library wants a mode and because a bundle copied to any other
// system should land with sane bits rather than 0666.
//
// See perm_windows.go for what actually holds the door shut.
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Defaults for a contributor who has not been assigned identifiers.
const (
	DefaultScenario  = "ADHOC"
	DefaultVolunteer = "vol-local"
)

// bundleIDPattern mirrors the naming convention in the existing capture manual
// so bundles from either source are indistinguishable at intake.
var bundleIDPattern = regexp.MustCompile(`^SC_[0-9]{8}T[0-9]{6}Z__[A-Z0-9-]+__[A-Za-z0-9-]+__[A-Z]*__[0-9]{3}$`)

var (
	scenarioSanitiser  = regexp.MustCompile(`[^A-Z0-9-]+`)
	volunteerSanitiser = regexp.MustCompile(`[^A-Za-z0-9-]+`)
	regionSanitiser    = regexp.MustCompile(`[^A-Z]+`)
)

// BundleID builds SC_<UTC>__<SCENARIO>__<VOLUNTEER>__<REGION>__<SEQ>.
func BundleID(start time.Time, scenario, volunteer, region string, seq int) string {
	scenario = scenarioSanitiser.ReplaceAllString(strings.ToUpper(strings.TrimSpace(scenario)), "-")
	scenario = strings.Trim(scenario, "-")
	if scenario == "" {
		scenario = DefaultScenario
	}
	// The manual writes scenario ids as SC-AUTH-02 but names bundles without
	// the prefix; accept either form from the contributor.
	scenario = strings.TrimPrefix(scenario, "SC-")

	volunteer = volunteerSanitiser.ReplaceAllString(strings.TrimSpace(volunteer), "-")
	volunteer = strings.Trim(volunteer, "-")
	if volunteer == "" {
		volunteer = DefaultVolunteer
	}

	region = regionSanitiser.ReplaceAllString(strings.ToUpper(strings.TrimSpace(region)), "")

	return fmt.Sprintf("SC_%s__%s__%s__%s__%03d",
		start.UTC().Format("20060102T150405Z"), scenario, volunteer, region, seq)
}

// ValidBundleID reports whether id matches the convention.
func ValidBundleID(id string) bool { return bundleIDPattern.MatchString(id) }

// Bundle is a session directory on disk.
type Bundle struct {
	Dir string
	ID  string
}

// CreateBundle makes the session directory, choosing the next free sequence
// number if an identically-named bundle already exists.
//
// It never overwrites or reuses an existing directory: prior sessions are not
// touched to make room for a new one (FR-036).
func CreateBundle(parent string, start time.Time, scenario, volunteer, region string) (*Bundle, error) {
	if err := os.MkdirAll(parent, DirMode); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	for seq := 0; seq < 1000; seq++ {
		id := BundleID(start, scenario, volunteer, region, seq)
		dir := filepath.Join(parent, id)
		err := os.Mkdir(dir, DirMode)
		if err == nil {
			// Install the owner-only DACL immediately, before a single byte is
			// written into the directory. The mode passed to Mkdir above did
			// not protect anything, and every file the capture is about to
			// create inherits its access from this call.
			if err := secureDir(dir); err != nil {
				return nil, fmt.Errorf("securing %s: %w", dir, err)
			}
			return &Bundle{Dir: dir, ID: id}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create bundle directory: %w", err)
		}
	}
	return nil, fmt.Errorf("no free sequence number for a bundle starting at %s", start.UTC())
}

// Path returns the path of a file inside the bundle.
func (b *Bundle) Path(name string) string { return filepath.Join(b.Dir, name) }

// CheckPermissions reports whether the bundle is readable only by its owner.
//
// `loose` names anything readable by others. Looser permissions are reported
// but are not a verification failure — a contributor may have relaxed them
// deliberately in order to share, and refusing to verify a bundle they can no
// longer fix would help nobody.
//
// `summary` names the principals that actually hold access, because a mode
// string here would be a lie — it would report 0700 on a directory the whole
// Users group can read.
func CheckPermissions(dir string) (summary string, loose []string, err error) {
	return inspectPermissions(dir)
}
