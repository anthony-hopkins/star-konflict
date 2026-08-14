package annotate

import (
	"fmt"
	"sync"
	"time"

	"github.com/sc-re/sc-capture/internal/decode"
)

// Deriver turns decoded traffic into timeline annotations.
//
// Sessions arrive from distributed contributors and are read by people who were
// not there. Because the system already decodes the traffic, it can label the
// interesting moments itself — which removes an entire class of manual
// annotation work, and the transcription errors that come with it (FR-019).
//
// Derived marks carry KindDerived so they are never confused with contributor
// testimony. A person saying "I bought a shield booster here" and a decoder
// saying "a handoff message appeared here" are different kinds of claim, and an
// archive that blurs them is less useful than one that does not.
type Deriver struct {
	beacon *Beacon

	mu            sync.Mutex
	seenHandoffs  int
	seenNovel     int
	seenDesyncs   int
	credentialled bool
}

// NewDeriver returns a deriver writing to a session's beacon.
func NewDeriver(b *Beacon) *Deriver { return &Deriver{beacon: b} }

// Observe stamps annotations for anything newly notable in stats.
//
// Called on a timer, so the annotation lands close to the traffic that caused
// it rather than being backfilled at close.
func (d *Deriver) Observe(stats decode.Stats, wall time.Time, mono int64) []Marker {
	d.mu.Lock()
	defer d.mu.Unlock()

	var stamped []Marker
	stamp := func(format string, args ...any) {
		m, err := d.beacon.Stamp(KindDerived, fmt.Sprintf(format, args...), wall, mono)
		if err == nil {
			stamped = append(stamped, m)
		}
	}

	for i := d.seenHandoffs; i < len(stats.Handoffs); i++ {
		h := stats.Handoffs[i]
		// The single most valuable moment a session can contain: the point
		// where the client is handed off to a match server.
		stamp("dedicated-server handoff to %s:%d, zone %d — in-match traffic starts here",
			h.Addr, h.Port, h.ZoneID)
	}
	d.seenHandoffs = len(stats.Handoffs)

	for i := d.seenNovel; i < len(stats.Novel); i++ {
		e := stats.Novel[i]
		stamp("novel %s id %d observed — not in this build's element tables", e.Kind, e.ID)
	}
	d.seenNovel = len(stats.Novel)

	if stats.Desyncs > d.seenDesyncs {
		stamp("stream desync (%d total) — bytes from here on are journaled but not decoded",
			stats.Desyncs)
		d.seenDesyncs = stats.Desyncs
	}

	if stats.CredentialsSeen && !d.credentialled {
		stamp("authentication traffic observed — this session contains credential material")
		d.credentialled = true
	}

	return stamped
}
