package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/decode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/exitcode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/index"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/journal"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/session"
)

// runIndex rebuilds the derived record index from the raw journal.
//
// This is what makes FR-030 a one-command operation: improve a decoder, rebuild,
// and records that previously read "undecoded" now decode — with the pcapng
// hashes unchanged, because the evidence was never touched.
func runIndex(args []string) int {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	rebuild := fs.Bool("rebuild", false, "regenerate index.jsonl from the pcapng segments")
	dir, perr := parsePositional(fs, args)
	if perr != nil || dir == "" {
		errf("usage: sccap index <bundle-dir> --rebuild")
		return exitcode.Usage
	}

	if !*rebuild {
		errf("nothing to do; pass --rebuild")
		return exitcode.Usage
	}

	meta, err := session.LoadMetadata(dir)
	if err != nil {
		if isSchemaError(err) {
			errf("%v", err)
			return exitcode.SchemaUnreadable
		}
		errf("%v", err)
		return exitcode.Usage
	}

	// Hash the raw journal before and after, and prove we did not touch it.
	before, err := hashSegments(dir)
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}

	w, err := index.Create(dir)
	if err != nil {
		errf("open index: %v", err)
		return exitcode.Usage
	}

	var count uint64
	_, stats, err := decode.Replay(dir, meta.UTCStart, func(r decode.Record) {
		if err := w.Write(r); err == nil {
			count++
		}
	})
	if err != nil {
		errf("replay: %v", err)
		_ = w.Close()
		return exitcode.Usage
	}
	if err := w.Close(); err != nil {
		errf("closing index: %v", err)
		return exitcode.Usage
	}

	after, err := hashSegments(dir)
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}
	for name, h := range before {
		if after[name] != h {
			// This should be impossible, and that is exactly why it is
			// checked: a rebuild that altered the evidence would destroy the
			// property the whole archive rests on.
			errf("RAW JOURNAL MODIFIED during rebuild: %s. This is a serious bug; "+
				"do not trust this bundle.", name)
			return exitcode.VerifyFailed
		}
	}

	fmt.Fprintf(os.Stderr, "Rebuilt %s: %d records from %d segment(s).\n",
		index.File, count, len(before))
	fmt.Fprintf(os.Stderr, "Raw journal unchanged (%d segment hashes verified).\n", len(before))
	if stats.Desyncs > 0 {
		fmt.Fprintf(os.Stderr, "%d stream desync(s); those bytes remain in the journal, "+
			"undecoded.\n", stats.Desyncs)
	}
	if len(stats.Novel) > 0 {
		fmt.Fprintf(os.Stderr, "%d novel element(s) observed.\n", len(stats.Novel))
	}
	fmt.Fprintln(os.Stderr, "\nSHA256SUMS now covers a changed index; refresh it with:")
	fmt.Fprintf(os.Stderr, "  sccap verify %s --write-sums\n", dir)
	return exitcode.OK
}

func hashSegments(dir string) (map[string]string, error) {
	names, err := decode.Segments(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		h, err := journal.HashFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out[n] = h
	}
	return out, nil
}

func isSchemaError(err error) bool {
	var e *session.ErrSchemaUnreadable
	return asError(err, &e)
}

func asError[T error](err error, target *T) bool {
	if v, ok := err.(T); ok {
		*target = v
		return true
	}
	return false
}
