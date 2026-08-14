// Package scproto is the single implementation of the Star Conflict wire
// protocol in this project: framing, message typing, and common value
// encodings.
//
// It depends on nothing else in this module and nothing outside the standard
// library, so that a future server reimplementation can consume it unchanged.
// An architecture test enforces that (Principle VI). The rule also stops a
// second parser appearing behind a convenience wrapper, which is the failure
// the principle actually exists to prevent.
package scproto

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

//go:embed tables/*.json
var tableFS embed.FS

// Kind distinguishes the three addressable element namespaces.
type Kind string

const (
	KindMessageType  Kind = "message_type"
	KindAsyncRequest Kind = "async_request"
	KindNotification Kind = "notification"
)

// Element is one named, addressable unit of the protocol whose observation is
// worth tracking.
//
// These are NAMES, not body layouts. The names were recovered almost in full
// from the game client's own string tables; the layouts mostly were not. That
// gap is precisely what coverage measures: an element here that has never
// appeared on the wire is a countdown item, and one observed but not decodable
// is safe forever.
type Element struct {
	Kind Kind   `json:"kind"`
	ID   uint16 `json:"id"`
	Name string `json:"name"`
}

// Key is the stable identifier used by the coverage store. Ids are never
// renumbered, so a key written today still resolves years later.
func (e Element) Key() string { return string(e.Kind) + ":" + itoa(int(e.ID)) }

type tableEntry struct {
	ID   uint16 `json:"id"`
	Name string `json:"name"`
}

var (
	tablesOnce sync.Once
	byKind     map[Kind]map[uint16]string
	allSorted  []Element
	tablesErr  error
)

func loadTables() {
	byKind = map[Kind]map[uint16]string{}

	for _, src := range []struct {
		kind Kind
		file string
	}{
		{KindMessageType, "tables/message-types.json"},
		{KindAsyncRequest, "tables/async-requests.json"},
		{KindNotification, "tables/notifications.json"},
	} {
		b, err := tableFS.ReadFile(src.file)
		if err != nil {
			tablesErr = fmt.Errorf("read %s: %w", src.file, err)
			return
		}
		var entries []tableEntry
		if err := json.Unmarshal(b, &entries); err != nil {
			tablesErr = fmt.Errorf("parse %s: %w", src.file, err)
			return
		}
		m := make(map[uint16]string, len(entries))
		for _, e := range entries {
			m[e.ID] = e.Name
			allSorted = append(allSorted, Element{Kind: src.kind, ID: e.ID, Name: e.Name})
		}
		byKind[src.kind] = m
	}

	sort.Slice(allSorted, func(i, j int) bool {
		if allSorted[i].Kind != allSorted[j].Kind {
			return allSorted[i].Kind < allSorted[j].Kind
		}
		return allSorted[i].ID < allSorted[j].ID
	})
}

func tables() (map[Kind]map[uint16]string, error) {
	tablesOnce.Do(loadTables)
	return byKind, tablesErr
}

// Name returns the element name for a kind and id, and whether it is known.
//
// An unknown id is not an error: it is a discovery. Traffic carrying an element
// absent from these tables is flagged as novel and surfaced, never dropped.
func Name(kind Kind, id uint16) (string, bool) {
	t, err := tables()
	if err != nil {
		return "", false
	}
	n, ok := t[kind][id]
	return n, ok
}

// Universe returns every known element, sorted by kind then id.
func Universe() ([]Element, error) {
	if _, err := tables(); err != nil {
		return nil, err
	}
	out := make([]Element, len(allSorted))
	copy(out, allSorted)
	return out, nil
}

// Counts returns how many elements are known per kind.
func Counts() (map[Kind]int, error) {
	t, err := tables()
	if err != nil {
		return nil, err
	}
	out := map[Kind]int{}
	for k, m := range t {
		out[k] = len(m)
	}
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
