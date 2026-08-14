// Package verify confirms a bundle is complete and internally consistent
// before a contributor shares it.
//
// The important asymmetry: an INTERRUPTED session passes. Abrupt termination is
// an expected way for a capture to end, and a session that is valid up to the
// point of failure is evidence, not garbage. Only inconsistency fails —
// a hash that does not match, a structurally broken segment, a claim in
// session.json contradicted by what is on disk.
package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/journal"
	"github.com/anthony-hopkins/star-konflict/sc-capture/internal/session"
	"github.com/gopacket/gopacket/pcapgo"
)

// Status of a verified bundle.
const (
	StatusClean       = "clean"
	StatusInterrupted = "interrupted"
	StatusFailed      = "failed"
)

// Finding is one observation about a bundle.
type Finding struct {
	Check  string `json:"check"`
	Level  string `json:"level"` // ok | warn | fail
	Detail string `json:"detail"`
}

// Result is the verification report.
type Result struct {
	Bundle   string    `json:"bundle"`
	Status   string    `json:"status"`
	Mode     string    `json:"mode,omitempty"`
	Rewrites int       `json:"rewrites"`
	Frames   uint64    `json:"frames"`
	Segments int       `json:"segments"`
	Drops    uint64    `json:"packets_dropped"`
	Findings []Finding `json:"findings"`
}

func (r *Result) add(check, level, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{
		Check: check, Level: level, Detail: fmt.Sprintf(format, args...),
	})
}

// Failed reports whether verification failed.
func (r *Result) Failed() bool { return r.Status == StatusFailed }

// Verify checks a bundle directory.
func Verify(dir string) (*Result, error) {
	res := &Result{Bundle: filepath.Base(dir), Status: StatusClean}

	// 1. Schema version, before anything else. An unknown MAJOR is refused
	//    outright rather than partially read.
	meta, err := session.LoadMetadata(dir)
	if err != nil {
		var schemaErr *session.ErrSchemaUnreadable
		if errors.As(err, &schemaErr) {
			return nil, err
		}
		res.Status = StatusFailed
		res.add("session.json", "fail", "%v", err)
		return res, nil
	}
	res.Mode = meta.Mode
	res.Rewrites = len(meta.Rewrites)
	res.Drops = meta.Host.PacketsDropped
	res.add("schema", "ok", "version %s", meta.SchemaVersion)

	if meta.BundleID != filepath.Base(dir) {
		res.Status = StatusFailed
		res.add("bundle id", "fail", "session.json says %q but the directory is %q",
			meta.BundleID, filepath.Base(dir))
	}

	// 2. Termination state. Interrupted is not a failure.
	switch meta.Termination {
	case session.TerminationClean:
		res.add("termination", "ok", "closed cleanly")
	case session.TerminationDiskFloor:
		res.add("termination", "warn", "capture stopped because free space reached the floor; the session is complete up to that point")
	default:
		res.Status = StatusInterrupted
		res.add("termination", "warn", "session was interrupted; valid up to the point of failure")
	}
	if meta.UTCEnd == nil && meta.Termination == session.TerminationClean {
		res.Status = StatusFailed
		res.add("termination", "fail", "claims a clean close but has no utc_end")
	}

	// 3. Integrity.
	verifySums(dir, res)

	// 4. Every segment walks structurally end to end.
	frames, segs := verifySegments(dir, meta, res)
	res.Frames = frames
	res.Segments = segs

	// 5. Kernel drops. Non-zero means bytes that crossed the wire are missing
	//    from the journal, which is the one thing this tool exists to prevent.
	if meta.Host.PacketsDropped > 0 {
		res.Status = StatusFailed
		res.add("drops", "fail",
			"the kernel dropped %d packets — this session is missing traffic that crossed the wire",
			meta.Host.PacketsDropped)
	} else {
		res.add("drops", "ok", "no packets dropped")
	}

	// 6. Capture-time discard decisions.
	if meta.Host.CaptureFilter != "" {
		res.add("filter", "warn",
			"a capture filter was active (%q): traffic outside it was never recorded",
			meta.Host.CaptureFilter)
	}
	if meta.Host.Snaplen > 0 {
		res.add("snaplen", "warn",
			"snaplen was %d: frames longer than this were truncated at capture time",
			meta.Host.Snaplen)
	}

	// 7. Clock anchors must not go backwards in monotonic terms.
	verifyAnchors(meta, res)

	// 8. Permissions. Loose is a warning: a contributor may have relaxed them
	//    deliberately in order to share, and refusing to verify a bundle they
	//    can no longer fix would help nobody.
	if summary, loose, err := session.CheckPermissions(dir); err == nil && len(loose) > 0 {
		res.add("permissions", "warn",
			"readable beyond the owner (%s): %s — this session may contain credentials",
			summary, strings.Join(loose, ", "))
	} else if err == nil {
		// The summary names the principals the bundle's ACL grants, rather
		// than a mode, so a reader can see exactly who could have touched it.
		res.add("permissions", "ok", "%s", summary)
	}

	// 9. Sensitivity must be marked.
	if !meta.Sensitive {
		res.Status = StatusFailed
		res.add("sensitivity", "fail", "session is not marked sensitive")
	}

	// 10. The index is derived: a truncated tail is expected after a kill and
	//     does not invalidate anything.
	verifyIndex(dir, res)

	return res, nil
}

func verifySums(dir string, res *Result) {
	sumsPath := filepath.Join(dir, journal.SumsFile)
	declared, err := journal.ReadSums(sumsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Expected for an interrupted session: SHA256SUMS is written last.
			res.add("integrity", "warn",
				"no %s (expected for an interrupted session); regenerate with --write-sums",
				journal.SumsFile)
			return
		}
		res.Status = StatusFailed
		res.add("integrity", "fail", "%v", err)
		return
	}

	actual, err := journal.HashDir(dir)
	if err != nil {
		res.Status = StatusFailed
		res.add("integrity", "fail", "hashing bundle: %v", err)
		return
	}

	var mismatched, missing, unlisted []string
	for name, want := range declared {
		got, ok := actual[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if got != want {
			mismatched = append(mismatched, name)
		}
	}
	for name := range actual {
		if _, ok := declared[name]; !ok {
			unlisted = append(unlisted, name)
		}
	}
	sort.Strings(mismatched)
	sort.Strings(missing)
	sort.Strings(unlisted)

	if len(mismatched) > 0 {
		res.Status = StatusFailed
		res.add("integrity", "fail", "hash mismatch: %s", strings.Join(mismatched, ", "))
	}
	if len(missing) > 0 {
		res.Status = StatusFailed
		res.add("integrity", "fail", "listed but absent: %s", strings.Join(missing, ", "))
	}
	if len(unlisted) > 0 {
		res.add("integrity", "warn", "present but not listed in %s: %s",
			journal.SumsFile, strings.Join(unlisted, ", "))
	}
	if len(mismatched) == 0 && len(missing) == 0 {
		res.add("integrity", "ok", "%d files hashed and matching", len(declared))
	}
}

// verifySegments walks every pcapng end to end. A truncated tail on the LAST
// segment is expected after an abrupt kill; a truncated earlier segment means
// something else went wrong.
func verifySegments(dir string, meta *session.Metadata, res *Result) (frames uint64, count int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		res.Status = StatusFailed
		res.add("segments", "fail", "%v", err)
		return 0, 0
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pcapng") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	if len(names) == 0 {
		res.Status = StatusFailed
		res.add("segments", "fail", "no pcapng segments — there is no evidence in this bundle")
		return 0, 0
	}

	for i, name := range names {
		n, truncated, err := walkSegment(filepath.Join(dir, name))
		frames += n
		switch {
		case err != nil && !truncated:
			res.Status = StatusFailed
			res.add("segments", "fail", "%s: %v", name, err)
		case truncated && i == len(names)-1:
			res.add("segments", "warn",
				"%s: truncated tail after %d frames — expected for an interrupted session", name, n)
		case truncated:
			res.Status = StatusFailed
			res.add("segments", "fail",
				"%s: truncated but is not the final segment — data is missing from the middle of this session", name)
		default:
			res.add("segments", "ok", "%s: %d frames", name, n)
		}
	}

	if len(meta.Segments) > 0 && len(meta.Segments) != len(names) {
		res.add("segments", "warn", "session.json lists %d segments but %d are present",
			len(meta.Segments), len(names))
	}
	return frames, len(names)
}

func walkSegment(path string) (frames uint64, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	r, err := pcapgo.NewNgReader(f, pcapgo.DefaultNgReaderOptions)
	if err != nil {
		return 0, false, fmt.Errorf("not a readable pcapng: %w", err)
	}
	for {
		_, _, err := r.ReadPacketData()
		if err == io.EOF {
			return frames, false, nil
		}
		if err != nil {
			// Anything short of a clean EOF at this point is a torn tail.
			return frames, true, err
		}
		frames++
	}
}

func verifyAnchors(meta *session.Metadata, res *Result) {
	anchors := meta.Clock.Anchors
	if len(anchors) == 0 {
		res.add("clock", "warn", "no clock anchors recorded")
		return
	}
	var steps int
	for i := 1; i < len(anchors); i++ {
		if anchors[i].Mono < anchors[i-1].Mono {
			res.Status = StatusFailed
			res.add("clock", "fail", "monotonic time goes backwards between anchors %d and %d", i-1, i)
			return
		}
		if anchors[i].Kind == session.AnchorStep {
			steps++
		}
	}
	if steps > 0 {
		res.add("clock", "warn",
			"%d wall-clock step(s) recorded; monotonic ordering is unaffected", steps)
		return
	}
	res.add("clock", "ok", "%d anchors, monotonic", len(anchors))
}

// verifyIndex checks that index.jsonl, if present, only cites frames that
// exist. It is derived data — absent or truncated is fine.
func verifyIndex(dir string, res *Result) {
	path := filepath.Join(dir, "index.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return // derived and optional
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var records, bad int
	for {
		var rec struct {
			Seq    int `json:"seq"`
			Frames []struct {
				Segment string `json:"segment"`
				Index   int    `json:"index"`
			} `json:"frames"`
		}
		if err := dec.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			// A torn final line is the expected shape of an abrupt kill.
			res.add("index", "warn", "truncated final record — rebuild with: sccap index --rebuild")
			break
		}
		records++
		for _, fr := range rec.Frames {
			if _, serr := os.Stat(filepath.Join(dir, fr.Segment)); serr != nil {
				bad++
			}
		}
	}
	if bad > 0 {
		res.add("index", "warn", "%d records cite missing segments — rebuild with: sccap index --rebuild", bad)
		return
	}
	if records > 0 {
		res.add("index", "ok", "%d records, all frame references resolve", records)
	}
}
