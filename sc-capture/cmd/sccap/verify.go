package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sc-re/sc-capture/internal/exitcode"
	"github.com/sc-re/sc-capture/internal/journal"
	"github.com/sc-re/sc-capture/internal/session"
	"github.com/sc-re/sc-capture/internal/verify"
)

func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit machine-readable output on stdout")
	writeSums := fs.Bool("write-sums", false, "generate SHA256SUMS if absent (records that it was generated post hoc)")
	dir, err := parsePositional(fs, args)
	if err != nil || dir == "" {
		errf("usage: sccap verify <bundle-dir> [--json] [--write-sums]")
		return exitcode.Usage
	}

	if *writeSums {
		if _, err := os.Stat(dir + "/" + journal.SumsFile); os.IsNotExist(err) {
			if err := journal.WriteSums(dir); err != nil {
				errf("writing %s: %v", journal.SumsFile, err)
				return exitcode.VerifyFailed
			}
			// The distinction between "hashed at clean close" and "hashed
			// afterwards" is worth keeping: the second proves the files match
			// each other now, not that they were complete when written.
			if meta, err := session.LoadMetadata(dir); err == nil {
				meta.Anomaly("SHA256SUMS generated post hoc by 'verify --write-sums', " +
					"not at session close")
				_ = meta.Write()
				_ = journal.WriteSums(dir)
			}
			fmt.Fprintf(os.Stderr, "Generated %s.\n", journal.SumsFile)
		}
	}

	res, err := verify.Verify(dir)
	if err != nil {
		var schemaErr *session.ErrSchemaUnreadable
		if errors.As(err, &schemaErr) {
			errf("%v", err)
			return exitcode.SchemaUnreadable
		}
		errf("%v", err)
		return exitcode.VerifyFailed
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			errf("%v", err)
			return exitcode.Usage
		}
	} else {
		fmt.Fprintf(os.Stderr, "%s\n\n", res.Bundle)
		for _, f := range res.Findings {
			mark := map[string]string{"ok": "  ok  ", " ": "      ", "warn": " warn ", "fail": " FAIL "}[f.Level]
			fmt.Fprintf(os.Stderr, "[%s] %-12s %s\n", mark, f.Check, f.Detail)
		}
		fmt.Fprintln(os.Stderr)
		switch res.Status {
		case verify.StatusClean:
			fmt.Fprintf(os.Stderr, "VERIFIED — %d frames across %d segment(s), mode=%s.\n",
				res.Frames, res.Segments, res.Mode)
			fmt.Fprintln(os.Stderr, "This session is complete and internally consistent.")
		case verify.StatusInterrupted:
			fmt.Fprintf(os.Stderr, "VERIFIED (interrupted) — %d frames across %d segment(s).\n",
				res.Frames, res.Segments)
			fmt.Fprintln(os.Stderr, "The session ended abruptly but is valid up to that point. It is worth keeping.")
		default:
			fmt.Fprintln(os.Stderr, "FAILED — see the FAIL lines above.")
		}
	}

	if res.Failed() {
		return exitcode.VerifyFailed
	}
	return exitcode.OK
}
