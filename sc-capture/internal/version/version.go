// Package version carries build identity. It is recorded into every session so
// that a bundle can be interpreted years later without asking its author
// (Principle V).
package version

import "runtime/debug"

// Version is the release version, overridable at build time with
// -ldflags "-X .../internal/version.Version=v0.2.0".
var Version = "0.1.0-dev"

// Commit is filled from the embedded VCS stamp when built from a git checkout.
var Commit = commitFromBuildInfo()

// TablesRevision identifies the embedded protocol element universe. It is
// recorded per session so a coverage claim can be traced to the table revision
// that produced it.
var TablesRevision = "sc-proxy@968f1a3f"

// UserAgent is what the pcapng section header records as the writing
// application.
func UserAgent() string {
	if Commit != "" {
		return "sccap " + Version + " (" + Commit + ")"
	}
	return "sccap " + Version
}

func commitFromBuildInfo() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return ""
}
