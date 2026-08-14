// Package exitcode defines the process exit codes that form part of sccap's
// contract with anything scripting it. See specs/001-capture-proxy/contracts/cli.md.
package exitcode

const (
	// OK means the command did what it was asked.
	OK = 0

	// Usage means the invocation was wrong: bad flag, missing argument.
	Usage = 1

	// VerifyFailed means a bundle failed verification — a hash mismatch, a
	// structural failure, or an inconsistency between files. It does NOT mean
	// the session was interrupted; an interrupted session verifies OK.
	VerifyFailed = 2

	// NoCapability means capture is impossible on this host: no Npcap backend
	// compiled in, Npcap not installed, or the process is not elevated.
	NoCapability = 3

	// DiskFloor means capture stopped because free space reached the hard
	// floor. The session was closed cleanly and is valid.
	DiskFloor = 4

	// SchemaUnreadable means a bundle declares a schema MAJOR this build does
	// not know. Never attempt a partial read of such a bundle.
	SchemaUnreadable = 5
)
