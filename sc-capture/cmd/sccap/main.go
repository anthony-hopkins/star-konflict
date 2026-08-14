// Command sccap archives the network footprint of a Star Conflict session.
//
// Output convention, which is part of the contract: data that might be piped
// goes to stdout; progress, warnings and diagnostics go to stderr. A
// contributor watching the screen and a script reading a pipe never contend.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/exitcode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/version"
)

const usage = `sccap — Star Conflict capture tooling (%s)

Usage: sccap <command> [flags]

Commands:
  doctor     Can this machine capture? What is missing? Which interface?
  capture    Record a session
  mark       Stamp a labelled moment onto a running session's timeline
  verify     Confirm a bundle is complete and internally consistent
  index      Rebuild the derived record index from the raw journal
  decode     Read an archived session (works with no server reachable)
  coverage   What protocol elements have never been observed?
  status     Snapshot of a running capture
  version    Print version and exit

Run "sccap <command> --help" for the flags of a command.

Start with "sccap doctor". With the game running, follow it with
"sccap doctor --watch 30s" to confirm which interface carries game traffic —
capturing the wrong one produces a session that passes every other check and
contains nothing useful.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, version.Version)
		os.Exit(exitcode.Usage)
	}

	args := os.Args[2:]
	switch os.Args[1] {
	case "doctor":
		os.Exit(runDoctor(args))
	case "capture":
		os.Exit(runCapture(args))
	case "mark":
		os.Exit(runMark(args))
	case "verify":
		os.Exit(runVerify(args))
	case "index":
		os.Exit(runIndex(args))
	case "decode":
		os.Exit(runDecode(args))
	case "coverage":
		os.Exit(runCoverage(args))
	case "status":
		os.Exit(runStatus(args))
	case "version":
		fmt.Println(version.UserAgent())
		fmt.Println("protocol tables:", version.TablesRevision)
		os.Exit(exitcode.OK)
	case "-h", "--help", "help":
		fmt.Fprintf(os.Stdout, usage, version.Version)
		os.Exit(exitcode.OK)
	default:
		fmt.Fprintf(os.Stderr, "sccap: unknown command %q\n\n", os.Args[1])
		fmt.Fprintf(os.Stderr, usage, version.Version)
		os.Exit(exitcode.Usage)
	}
}

// errf reports a fatal problem to stderr in a consistent shape.
func errf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sccap: "+format+"\n", args...)
}

// parsePositional parses flags that may appear on either side of a single
// positional argument.
//
// Go's flag package stops at the first positional, so "verify <dir> --json"
// would silently ignore --json — and that is the order people naturally type.
// Rather than guess which flags take values, this lets the FlagSet parse
// repeatedly and consumes one positional per pass; the FlagSet already knows
// which of its flags are booleans.
func parsePositional(fs *flag.FlagSet, args []string) (string, error) {
	var positional string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return "", err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		if positional == "" {
			positional = fs.Arg(0)
		} else {
			return "", fmt.Errorf("unexpected extra argument %q", fs.Arg(0))
		}
		rest = fs.Args()[1:]
		if len(rest) == 0 {
			return positional, nil
		}
	}
}
