package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sc-re/sc-capture/internal/coverage"
	"github.com/sc-re/sc-capture/internal/exitcode"
	"github.com/sc-re/sc-capture/internal/session"
	"github.com/sc-re/sc-capture/pkg/scproto"
)

func runCoverage(args []string) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit machine-readable output on stdout")
	kind := fs.String("kind", "", "message_type | async_request | notification")
	state := fs.String("state", "", "never_observed | observed_undecoded | decoded")
	ingest := fs.String("ingest", "", "fold a bundle's coverage-delta.json into machine-wide state")
	if err := fs.Parse(args); err != nil {
		return exitcode.Usage
	}

	doc, err := coverage.Load(session.DataDir())
	if err != nil {
		errf("%v", err)
		return exitcode.Usage
	}

	if *ingest != "" {
		n, err := ingestDelta(doc, *ingest)
		if err != nil {
			errf("%v", err)
			return exitcode.Usage
		}
		if err := doc.Save(); err != nil {
			errf("saving coverage: %v", err)
			return exitcode.Usage
		}
		fmt.Fprintf(os.Stderr, "Folded %d observation(s) from %s.\n", n, filepath.Base(*ingest))
	}

	if *kind != "" || *state != "" {
		entries := doc.Filter(*kind, *state)
		if *asJSON {
			return emitJSON(entries)
		}
		for _, e := range entries {
			fmt.Printf("%-16s %4d  %-12s %s\n", e.Kind, e.ID, e.State, e.Name)
		}
		fmt.Fprintf(os.Stderr, "\n%d element(s).\n", len(entries))
		return exitcode.OK
	}

	summary := doc.Summarise()
	novel := doc.NovelEntries()

	if *asJSON {
		return emitJSON(map[string]any{"summary": summary, "novel": novel})
	}

	var known, never int
	for _, s := range summary {
		known += s.Known
		never += s.NeverObserved
	}

	fmt.Fprintf(os.Stderr, "Element coverage (%d known)\n", known)
	for _, s := range summary {
		fmt.Fprintf(os.Stderr, "  %-16s %4d known  %4d decoded  %4d undecoded  %4d never observed\n",
			s.Kind, s.Known, s.Decoded, s.Undecoded, s.NeverObserved)
	}
	fmt.Fprintln(os.Stderr)

	// The framing here is deliberate. "Never observed" is the only number with
	// a deadline attached: an element captured but not understood is safe
	// forever, whereas one never captured dies with the servers.
	fmt.Fprintf(os.Stderr, "Never observed: %d — these die with the servers if nobody captures them.\n", never)
	fmt.Fprintln(os.Stderr, "  List them:  sccap coverage --state never_observed")
	if len(novel) > 0 {
		fmt.Fprintf(os.Stderr, "\nNovel elements seen but unknown to this build: %d\n", len(novel))
		for _, n := range novel {
			fmt.Fprintf(os.Stderr, "  %s id %d  first seen in %s\n", n.Kind, n.ID, n.FirstSeenSession)
		}
	}
	return exitcode.OK
}

func ingestDelta(doc *coverage.Document, bundleDir string) (int, error) {
	b, err := os.ReadFile(filepath.Join(bundleDir, coverage.DeltaFile))
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", coverage.DeltaFile, err)
	}
	var d coverage.Delta
	if err := json.Unmarshal(b, &d); err != nil {
		return 0, fmt.Errorf("parsing %s: %w", coverage.DeltaFile, err)
	}

	now := time.Now().UTC()
	n := 0
	for _, e := range d.Observed {
		doc.Observe(scproto.Kind(e.Kind), e.ID, e.State == coverage.Decoded, d.BundleID, now)
		n++
	}
	for _, e := range d.Novel {
		doc.Novelty(scproto.Kind(e.Kind), e.ID, e.Name, d.BundleID, now)
		n++
	}
	return n, nil
}

func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		errf("%v", err)
		return exitcode.Usage
	}
	return exitcode.OK
}
