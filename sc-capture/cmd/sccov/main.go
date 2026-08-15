// Command sccov serves the Star Conflict capture-coverage dashboard.
//
// It is monolithic on purpose: pure Go standard library, no external
// dependencies, and the entire front-end (HTML/CSS/JS) is embedded, so it
// compiles to a single static binary with nothing to install on the server.
//
// It reads the machine-wide coverage store written by sccap (coverage.json)
// and, optionally, a bundles directory for the per-session table, then renders
// a self-contained dashboard live on each request.
//
// It never exposes capture contents: only aggregate coverage (element names,
// states, counts) and non-sensitive per-session stats (scenario, region, frame
// count). No pcapng, no session tokens, no credentials ever reach the page.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard.html
var page string

// ---- coverage store (written by sccap) ----

type element struct {
	Kind             string `json:"kind"`
	ID               int    `json:"id"`
	Name             string `json:"name"`
	State            string `json:"state"`
	FirstSeenSession string `json:"first_seen_session"`
	FirstSeenUTC     string `json:"first_seen_utc"`
	Observations     int    `json:"observations"`
}

type store struct {
	Elements map[string]element `json:"elements"`
	Novel    []element          `json:"novel"`
	Updated  string             `json:"updated_utc"`
}

// ---- payload injected into the page ----

type totals struct {
	Known     int     `json:"known"`
	Decoded   int     `json:"decoded"`
	Undecoded int     `json:"undecoded"`
	Never     int     `json:"never"`
	Observed  int     `json:"observed"`
	Pct       float64 `json:"pct"`
}

type kindRow struct {
	Label     string `json:"label"`
	Code      string `json:"code"`
	Known     int    `json:"known"`
	Decoded   int    `json:"decoded"`
	Undecoded int    `json:"undecoded"`
}

type bundleRow struct {
	Scen   string `json:"scen"`
	Region string `json:"region"`
	Frames int    `json:"frames"`
	Seen   int    `json:"seen"`
	Novel  int    `json:"novel"`
	First  bool   `json:"first"`
}

// journeyPt marshals to a [label, value] tuple for the front-end.
type journeyPt struct {
	Label string
	Value int
}

func (j journeyPt) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]any{j.Label, j.Value})
}

type payload struct {
	Generated   string      `json:"generated"`
	Totals      totals      `json:"totals"`
	Kinds       []kindRow   `json:"kinds"`
	Bundles     []bundleRow `json:"bundles"`
	Journey     []journeyPt `json:"journey"`
	NovelCount  int         `json:"novelCount"`
	Sessions    int         `json:"sessions"`
	FramesTotal int         `json:"framesTotal"`
	HasBundles  bool        `json:"hasBundles"`
}

// kind display metadata, in canonical order.
var kindMeta = []struct{ key, label, prefix string }{
	{"message_type", "Message types", "cmd_type"},
	{"async_request", "Async requests", "AC_*"},
	{"notification", "Notifications", "SN_*"},
}

func scenarioOf(bundle string) (scen, region string) {
	parts := strings.Split(bundle, "__")
	if len(parts) > 1 {
		scen = parts[1]
	}
	if len(parts) > 3 {
		region = parts[3]
	}
	return
}

func build(st *store, bundlesDir, summaryPath string) payload {
	// per-kind + totals
	type c struct{ known, dec, und int }
	counts := map[string]*c{}
	for _, e := range st.Elements {
		k := counts[e.Kind]
		if k == nil {
			k = &c{}
			counts[e.Kind] = k
		}
		k.known++
		switch e.State {
		case "decoded":
			k.dec++
		case "observed_undecoded":
			k.und++
		}
	}
	var kinds []kindRow
	var t totals
	for _, m := range kindMeta {
		k := counts[m.key]
		if k == nil {
			k = &c{}
		}
		kinds = append(kinds, kindRow{
			Label: m.label, Code: m.prefix + " · " + itoa(k.known),
			Known: k.known, Decoded: k.dec, Undecoded: k.und,
		})
		t.Known += k.known
		t.Decoded += k.dec
		t.Undecoded += k.und
	}
	t.Never = t.Known - t.Decoded - t.Undecoded
	t.Observed = t.Decoded + t.Undecoded
	if t.Known > 0 {
		t.Pct = float64(int(1000*float64(t.Observed)/float64(t.Known))) / 10
	}

	// journey: group observed known-elements by first-seen session, ordered by time
	type sess struct {
		name, utc string
		count     int
	}
	sm := map[string]*sess{}
	for _, e := range st.Elements {
		if e.State == "never_observed" || e.FirstSeenSession == "" {
			continue
		}
		s := sm[e.FirstSeenSession]
		if s == nil {
			s = &sess{name: e.FirstSeenSession, utc: e.FirstSeenUTC}
			sm[e.FirstSeenSession] = s
		}
		s.count++
		if e.FirstSeenUTC != "" && (s.utc == "" || e.FirstSeenUTC < s.utc) {
			s.utc = e.FirstSeenUTC
		}
	}
	var sessions []*sess
	for _, s := range sm {
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].utc < sessions[j].utc })
	journey := []journeyPt{{Label: "Start", Value: t.Known}}
	never := t.Known
	for _, s := range sessions {
		never -= s.count
		scen, _ := scenarioOf(s.name)
		if scen == "" {
			scen = "?"
		}
		journey = append(journey, journeyPt{Label: scen, Value: never})
	}
	// Collapse consecutive same-scenario steps (e.g. two AUTH-01 logins) so the
	// countdown reads one step per scenario rather than repeating a label.
	if len(journey) > 1 {
		merged := journey[:1]
		for _, p := range journey[1:] {
			if merged[len(merged)-1].Label == p.Label {
				merged[len(merged)-1].Value = p.Value
				continue
			}
			merged = append(merged, p)
		}
		journey = merged
	}

	pl := payload{
		Generated:  time.Now().UTC().Format("2006-01-02 15:04 MST"),
		Totals:     t,
		Kinds:      kinds,
		Journey:    journey,
		NovelCount: len(st.Novel),
		Sessions:   len(sessions),
	}

	// optional per-session table. A pre-sanitized summary file (safe to deploy
	// publicly) wins over reading a raw packet-caps directory (local only).
	var bundles []bundleRow
	var frames int
	switch {
	case summaryPath != "":
		bundles, frames = readSummary(summaryPath)
	case bundlesDir != "":
		bundles, frames = readBundles(bundlesDir)
	}
	if len(bundles) > 0 {
		pl.Bundles = bundles
		pl.FramesTotal = frames
		pl.HasBundles = true
		pl.Sessions = len(bundles)
	}
	return pl
}

// readSummary loads a sanitized per-session summary: a JSON array of
// {scen, region, frames, seen, novel}. It carries no capture contents, so it is
// safe to commit and deploy to a public server.
func readSummary(path string) ([]bundleRow, int) {
	var rows []bundleRow
	readJSON(path, &rows)
	total, max, mi := 0, -1, -1
	for i := range rows {
		total += rows[i].Frames
		if rows[i].Seen > max {
			max, mi = rows[i].Seen, i
		}
	}
	if mi >= 0 {
		rows[mi].First = true
	}
	return rows, total
}

func readBundles(dir string) ([]bundleRow, int) {
	entries, _ := filepath.Glob(filepath.Join(dir, "SC_*"))
	sort.Strings(entries)
	var rows []bundleRow
	total := 0
	for _, d := range entries {
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			continue
		}
		name := filepath.Base(d)
		scen, region := scenarioOf(name)

		var delta struct {
			Observed []json.RawMessage `json:"observed"`
			Novel    []json.RawMessage `json:"novel"`
		}
		readJSON(filepath.Join(d, "coverage-delta.json"), &delta)

		var sj struct {
			Segments []struct {
				Frames int `json:"frames"`
			} `json:"segments"`
		}
		readJSON(filepath.Join(d, "session.json"), &sj)
		frames := 0
		for _, s := range sj.Segments {
			frames += s.Frames
		}
		total += frames

		rows = append(rows, bundleRow{
			Scen: scen, Region: region, Frames: frames,
			Seen: len(delta.Observed), Novel: len(delta.Novel),
		})
	}
	// highlight the biggest contributor
	max, mi := -1, -1
	for i, r := range rows {
		if r.Seen > max {
			max, mi = r.Seen, i
		}
	}
	if mi >= 0 {
		rows[mi].First = true
	}
	return rows, total
}

func readJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, v)
}

func loadStore(path string) (*store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st store
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func defaultStore() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "sccap", "coverage.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "coverage.json"
	}
	return filepath.Join(home, ".local", "share", "sccap", "coverage.json")
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	cov := flag.String("coverage", defaultStore(), "path to the sccap coverage.json store")
	bundles := flag.String("bundles", "", "optional packet-caps directory for the per-session table (local use)")
	summary := flag.String("bundles-summary", "", "optional sanitized per-session summary JSON (safe for public deploy)")
	emit := flag.String("emit-summary", "", "read a packet-caps dir, print the sanitized per-session summary JSON to stdout, and exit")
	cert := flag.String("tls-cert", "", "TLS certificate (enables HTTPS with -tls-key)")
	key := flag.String("tls-key", "", "TLS private key")
	flag.Parse()

	// One-shot: generate the sanitized summary that a public deploy serves,
	// straight from a local packet-caps directory. No capture contents leave —
	// only scenario, region, frame count, and observed/novel tallies.
	if *emit != "" {
		rows, _ := readBundles(*emit)
		b, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			log.Fatalf("emit-summary: %v", err)
		}
		os.Stdout.Write(append(b, '\n'))
		return
	}

	var (
		mu       sync.Mutex
		cached   string
		cachedAt time.Time
	)
	render := func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached != "" && time.Since(cachedAt) < 5*time.Second {
			return cached, nil
		}
		st, err := loadStore(*cov)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(build(st, *bundles, *summary))
		if err != nil {
			return "", err
		}
		// json.Marshal escapes <, >, & by default, so it is safe inside <script>.
		out := strings.Replace(page, "__SCCOV_DATA__", string(b), 1)
		cached, cachedAt = out, time.Now()
		return out, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := loadStore(*cov); err != nil {
			http.Error(w, "coverage store unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := render()
		if err != nil {
			http.Error(w, "coverage store unavailable: "+err.Error()+
				"\ncopy the sccap coverage.json to this server, or pass -coverage <path>", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=5")
		w.Write([]byte(html))
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if *cert != "" && *key != "" {
		log.Printf("sccov serving HTTPS on %s (coverage=%s)", *addr, *cov)
		log.Fatal(srv.ListenAndServeTLS(*cert, *key))
	}
	log.Printf("sccov serving HTTP on %s (coverage=%s)", *addr, *cov)
	log.Fatal(srv.ListenAndServe())
}
