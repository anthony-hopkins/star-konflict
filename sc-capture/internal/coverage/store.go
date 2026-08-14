// Package coverage tracks which protocol elements have never been observed.
//
// This is the project's progress metric against a hard external deadline, not
// diagnostics. An opcode captured but not understood is safe forever; an opcode
// never captured dies with the servers. Knowing which is which is what turns
// capture from an open-ended chore into a list that gets shorter (Principle III).
package coverage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthony-hopkins/star-konflict/sc-capture/pkg/scproto"
)

// SchemaVersion of the coverage document.
const SchemaVersion = "1.0"

// File is the machine-wide store's name.
const File = "coverage.json"

// DeltaFile is written into each bundle, so machine-wide coverage can be
// rebuilt from bundles alone if the store is ever lost.
const DeltaFile = "coverage-delta.json"

// State is an element's position in a strictly one-directional lattice:
//
//	never_observed -> observed_undecoded -> decoded
//
// State never regresses. A later session that fails to decode an
// already-decoded element does not downgrade it — a metric that moved backwards
// on a bad session would be untrustworthy, which is worse than not having one.
type State string

const (
	NeverObserved     State = "never_observed"
	ObservedUndecoded State = "observed_undecoded"
	Decoded           State = "decoded"
)

func rank(s State) int {
	switch s {
	case Decoded:
		return 2
	case ObservedUndecoded:
		return 1
	default:
		return 0
	}
}

// Entry is one element's record.
type Entry struct {
	Kind             string    `json:"kind"`
	ID               uint16    `json:"id"`
	Name             string    `json:"name"`
	State            State     `json:"state"`
	FirstSeenSession string    `json:"first_seen_session,omitempty"`
	FirstSeenUTC     time.Time `json:"first_seen_utc,omitempty"`
	Observations     uint64    `json:"observations"`
}

// Document is the on-disk coverage state.
type Document struct {
	SchemaVersion string            `json:"schema_version"`
	Elements      map[string]*Entry `json:"elements"`
	Novel         []Entry           `json:"novel"`
	UpdatedUTC    time.Time         `json:"updated_utc"`

	mu   sync.Mutex
	path string
}

// Load reads the machine-wide store, creating it from the embedded element
// universe on first use.
func Load(dir string) (*Document, error) {
	path := filepath.Join(dir, File)
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		d, err := bootstrap()
		if err != nil {
			return nil, err
		}
		d.path = path
		return d, nil
	}

	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", File, err)
	}
	d.path = path
	if d.Elements == nil {
		d.Elements = map[string]*Entry{}
	}

	// Fold in any elements this build knows about that the stored document
	// predates. Additive only — nothing is renumbered or removed, because the
	// keys are what past sessions cited.
	universe, err := scproto.Universe()
	if err != nil {
		return nil, err
	}
	for _, e := range universe {
		if _, ok := d.Elements[e.Key()]; !ok {
			d.Elements[e.Key()] = &Entry{
				Kind: string(e.Kind), ID: e.ID, Name: e.Name, State: NeverObserved,
			}
		}
	}
	return &d, nil
}

func bootstrap() (*Document, error) {
	universe, err := scproto.Universe()
	if err != nil {
		return nil, err
	}
	d := &Document{
		SchemaVersion: SchemaVersion,
		Elements:      make(map[string]*Entry, len(universe)),
	}
	for _, e := range universe {
		d.Elements[e.Key()] = &Entry{
			Kind: string(e.Kind), ID: e.ID, Name: e.Name, State: NeverObserved,
		}
	}
	return d, nil
}

// Observe records that an element was seen. decoded reports whether its body
// was understood.
func (d *Document) Observe(kind scproto.Kind, id uint16, decoded bool, sessionID string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := scproto.Element{Kind: kind, ID: id}.Key()
	e, ok := d.Elements[key]
	if !ok {
		name, known := scproto.Name(kind, id)
		if !known {
			name = fmt.Sprintf("%s_UNKNOWN_%d", strings.ToUpper(string(kind)), id)
		}
		e = &Entry{Kind: string(kind), ID: id, Name: name, State: NeverObserved}
		d.Elements[key] = e
	}

	e.Observations++
	if e.FirstSeenSession == "" {
		e.FirstSeenSession = sessionID
		e.FirstSeenUTC = at.UTC()
	}

	next := ObservedUndecoded
	if decoded {
		next = Decoded
	}
	if rank(next) > rank(e.State) {
		e.State = next
	}
}

// Novelty records an element that is not in this build's known universe.
func (d *Document) Novelty(kind scproto.Kind, id uint16, name, sessionID string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.Novel {
		if d.Novel[i].Kind == string(kind) && d.Novel[i].ID == id {
			d.Novel[i].Observations++
			return
		}
	}
	d.Novel = append(d.Novel, Entry{
		Kind: string(kind), ID: id, Name: name, State: ObservedUndecoded,
		FirstSeenSession: sessionID, FirstSeenUTC: at.UTC(), Observations: 1,
	})
}

// Summary counts elements by kind and state.
type Summary struct {
	Kind          string `json:"kind"`
	Known         int    `json:"known"`
	Decoded       int    `json:"decoded"`
	Undecoded     int    `json:"observed_undecoded"`
	NeverObserved int    `json:"never_observed"`
}

// Summarise returns per-kind counts in a stable order.
func (d *Document) Summarise() []Summary {
	d.mu.Lock()
	defer d.mu.Unlock()

	order := []string{string(scproto.KindMessageType), string(scproto.KindAsyncRequest),
		string(scproto.KindNotification)}
	idx := map[string]*Summary{}
	out := make([]Summary, 0, len(order))
	for _, k := range order {
		out = append(out, Summary{Kind: k})
	}
	for i := range out {
		idx[out[i].Kind] = &out[i]
	}

	for _, e := range d.Elements {
		s, ok := idx[e.Kind]
		if !ok {
			continue
		}
		s.Known++
		switch e.State {
		case Decoded:
			s.Decoded++
		case ObservedUndecoded:
			s.Undecoded++
		default:
			s.NeverObserved++
		}
	}
	return out
}

// Filter returns entries matching a kind and/or state; empty means any.
func (d *Document) Filter(kind, state string) []Entry {
	d.mu.Lock()
	defer d.mu.Unlock()

	var out []Entry
	for _, e := range d.Elements {
		if kind != "" && e.Kind != kind {
			continue
		}
		if state != "" && string(e.State) != state {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// NovelEntries returns elements unknown to this build.
func (d *Document) NovelEntries() []Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Entry, len(d.Novel))
	copy(out, d.Novel)
	return out
}

// Save writes the store by atomic replace, so a crash mid-write cannot leave a
// corrupt document. Cheap enough at this size that no other mechanism is
// warranted.
func (d *Document) Save() error {
	d.mu.Lock()
	d.UpdatedUTC = time.Now().UTC()
	d.SchemaVersion = SchemaVersion
	b, err := json.MarshalIndent(d, "", " ")
	path := d.path
	d.mu.Unlock()
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("coverage store has no path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Delta is what one session contributed.
type Delta struct {
	SchemaVersion string  `json:"schema_version"`
	BundleID      string  `json:"bundle_id"`
	Observed      []Entry `json:"observed"`
	Novel         []Entry `json:"novel"`
}

// WriteDelta records a session's contribution inside its own bundle.
//
// A session that contributed nothing still gets a delta with empty lists rather
// than nulls: "this session observed no known elements" is a fact worth
// recording, and it reads differently from "nobody looked".
func WriteDelta(bundleDir, bundleID string, observed, novel []Entry) error {
	if observed == nil {
		observed = []Entry{}
	}
	if novel == nil {
		novel = []Entry{}
	}
	d := Delta{SchemaVersion: SchemaVersion, BundleID: bundleID,
		Observed: observed, Novel: novel}
	b, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, DeltaFile), append(b, '\n'), 0o600)
}

// ParseKey splits an element key back into kind and id.
func ParseKey(key string) (scproto.Kind, uint16, bool) {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return "", 0, false
	}
	kind := scproto.Kind(key[:i])
	var id uint16
	if _, err := fmt.Sscanf(strings.TrimSuffix(key[i+1:], "!"), "%d", &id); err != nil {
		return "", 0, false
	}
	return kind, id, true
}
