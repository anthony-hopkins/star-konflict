package coverage

import (
	"testing"
	"time"

	"github.com/sc-re/sc-capture/pkg/scproto"
)

// TestStateNeverRegresses is the property the whole metric depends on.
//
// If a later session that fails to decode an element could downgrade it, the
// never-observed count would move backwards on a bad capture — and a progress
// metric that goes backwards for the wrong reasons is worse than none, because
// people would stop trusting the number that tells them what is still missing.
func TestStateNeverRegresses(t *testing.T) {
	d, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	now := time.Now().UTC()

	d.Observe(scproto.KindAsyncRequest, 9, true, "session-one", now)
	if got := stateOf(t, d, scproto.KindAsyncRequest, 9); got != Decoded {
		t.Fatalf("after a decode, state = %q, want %q", got, Decoded)
	}

	// A later session sees the same element but cannot decode it.
	d.Observe(scproto.KindAsyncRequest, 9, false, "session-two", now.Add(time.Hour))
	if got := stateOf(t, d, scproto.KindAsyncRequest, 9); got != Decoded {
		t.Errorf("state regressed to %q after a failed decode; the lattice is "+
			"one-directional", got)
	}

	// And never-observed must still promote normally.
	d.Observe(scproto.KindNotification, 3, false, "session-two", now)
	if got := stateOf(t, d, scproto.KindNotification, 3); got != ObservedUndecoded {
		t.Errorf("state = %q, want %q", got, ObservedUndecoded)
	}
	d.Observe(scproto.KindNotification, 3, true, "session-three", now)
	if got := stateOf(t, d, scproto.KindNotification, 3); got != Decoded {
		t.Errorf("state = %q, want promotion to %q", got, Decoded)
	}
}

// TestFirstSeenIsStable: attribution must point at the session that actually
// found something first, because that is what tells a contributor their capture
// mattered.
func TestFirstSeenIsStable(t *testing.T) {
	d, _ := Load(t.TempDir())
	now := time.Now().UTC()

	d.Observe(scproto.KindMessageType, 11, false, "first", now)
	d.Observe(scproto.KindMessageType, 11, true, "second", now.Add(time.Hour))

	e := entryOf(t, d, scproto.KindMessageType, 11)
	if e.FirstSeenSession != "first" {
		t.Errorf("first_seen_session = %q, want \"first\"", e.FirstSeenSession)
	}
	if e.Observations != 2 {
		t.Errorf("observations = %d, want 2", e.Observations)
	}
}

// TestBootstrapCoversTheWholeUniverse: every known element must start life
// counted as never observed, or the deadline metric under-reports what is
// missing.
func TestBootstrapCoversTheWholeUniverse(t *testing.T) {
	d, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var known, never int
	for _, s := range d.Summarise() {
		known += s.Known
		never += s.NeverObserved
	}
	if known != 404 {
		t.Errorf("known elements = %d, want 404", known)
	}
	if never != known {
		t.Errorf("%d of %d elements start as never observed; a fresh store must "+
			"count everything as missing", never, known)
	}
}

// TestPersistenceAcrossRestart: coverage aggregates across every session on the
// machine and survives restarts (FR-024).
func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	d1, _ := Load(dir)
	d1.Observe(scproto.KindAsyncRequest, 42, true, "s1", now)
	if err := d1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	d2, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := stateOf(t, d2, scproto.KindAsyncRequest, 42); got != Decoded {
		t.Errorf("state after restart = %q, want %q", got, Decoded)
	}
}

// TestNoveltyIsDeduplicated: a novel element seen a hundred times is one
// discovery, not a hundred.
func TestNoveltyIsDeduplicated(t *testing.T) {
	d, _ := Load(t.TempDir())
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		d.Novelty(scproto.KindAsyncRequest, 60000, "AC_UNKNOWN_60000", "s1", now)
	}
	novel := d.NovelEntries()
	if len(novel) != 1 {
		t.Fatalf("%d novel entries, want 1", len(novel))
	}
	if novel[0].Observations != 5 {
		t.Errorf("observations = %d, want 5", novel[0].Observations)
	}
}

func stateOf(t *testing.T, d *Document, kind scproto.Kind, id uint16) State {
	t.Helper()
	return entryOf(t, d, kind, id).State
}

func entryOf(t *testing.T, d *Document, kind scproto.Kind, id uint16) Entry {
	t.Helper()
	for _, e := range d.Filter(string(kind), "") {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no entry for %s %d", kind, id)
	return Entry{}
}
