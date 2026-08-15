package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/coverage"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/decode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/exitcode"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/index"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/journal"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/session"
	"github.com/anthony-hopkins/star-konflict/sc-capture/pkg/scproto"
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

	// Refresh the bundle's coverage contribution from this fresh decode. A
	// rebuild exists precisely so a decoder improvement is reflected without
	// re-capturing; that improvement must reach machine-wide coverage, not only
	// index.jsonl. The machine store is left alone — it is rebuilt by
	// re-ingesting the deltas.
	if err := refreshDelta(dir, meta.BundleID, meta.UTCStart, stats); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not refresh %s: %v\n", coverage.DeltaFile, err)
	}

	// The rebuild just rewrote index.jsonl and coverage-delta.json, both of which
	// SHA256SUMS covers. Refresh the manifest so the bundle stays verifiable. This
	// is safe precisely because the raw-journal hashes were checked unchanged
	// above: only derived-file hashes move; the evidence entries are identical.
	sumsRefreshed := true
	if err := journal.WriteSums(dir); err != nil {
		sumsRefreshed = false
		fmt.Fprintf(os.Stderr, "warning: could not refresh %s: %v\n", journal.SumsFile, err)
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
	if sumsRefreshed {
		fmt.Fprintln(os.Stderr, "SHA256SUMS refreshed for the rebuilt derived files "+
			"(evidence hashes unchanged).")
	} else {
		fmt.Fprintf(os.Stderr, "\nSHA256SUMS not refreshed; do it with:\n  sccap verify %s --write-sums\n", dir)
	}
	return exitcode.OK
}

// refreshDelta rewrites the bundle's coverage-delta.json from a fresh decode,
// so a decoder improvement reaches machine-wide coverage after a rebuild and not
// only index.jsonl. It mirrors capture-time coverage recording but deliberately
// does not touch the machine store: that is rebuilt by re-ingesting the deltas,
// which is the only way a corrected id can supersede a stale one (the store
// never regresses in place).
func refreshDelta(dir, bundleID string, start time.Time, stats decode.Stats) error {
	var observed []coverage.Entry
	for key := range stats.ObservedElements {
		kind, id, ok := coverage.ParseKey(key)
		if !ok {
			continue
		}
		decoded := strings.HasSuffix(key, "!")
		name, _ := scproto.Name(kind, id)
		state := coverage.ObservedUndecoded
		if decoded {
			state = coverage.Decoded
		}
		observed = append(observed, coverage.Entry{
			Kind: string(kind), ID: id, Name: name, State: state,
			FirstSeenSession: bundleID, FirstSeenUTC: start, Observations: 1,
		})
	}
	var novel []coverage.Entry
	for _, e := range stats.Novel {
		novel = append(novel, coverage.Entry{
			Kind: string(e.Kind), ID: e.ID, Name: e.Name,
			State: coverage.ObservedUndecoded, FirstSeenSession: bundleID,
			FirstSeenUTC: start, Observations: 1,
		})
	}
	return coverage.WriteDelta(dir, bundleID, observed, novel)
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
