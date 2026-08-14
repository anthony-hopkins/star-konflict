package session

import (
	"sync"
	"time"
)

// AnchorKind describes why a clock anchor was recorded.
type AnchorKind string

const (
	AnchorStart    AnchorKind = "start"
	AnchorPeriodic AnchorKind = "periodic"
	AnchorStep     AnchorKind = "step_detected"
	AnchorEnd      AnchorKind = "end"
)

// Anchor is a paired reading of both clocks.
//
// pcapng frames carry only a wall-clock timestamp, and the kernel hands us a
// realtime one. Anchors let monotonic time be recovered for any frame by
// interpolation, and — the point of the exercise — make a wall-clock step
// visible as a discontinuity between anchors instead of a silently corrupted
// timeline.
type Anchor struct {
	Wall time.Time  `json:"t_wall"`
	Mono int64      `json:"t_mono"` // nanoseconds since session start
	Kind AnchorKind `json:"kind"`
}

// AnchorInterval is how often a periodic anchor is recorded.
const AnchorInterval = 30 * time.Second

// StepThreshold is how far wall-clock may drift from monotonic before we call
// it a step rather than ordinary drift. NTP slewing stays well under this;
// a step (ntpdate, a VM resume, a manual set) blows straight past it.
const StepThreshold = time.Second

// Clock tracks both time sources for one session and records anchors.
//
// The zero value is not usable; call NewClock.
type Clock struct {
	mu sync.Mutex

	// start carries Go's monotonic reading, so time.Since(start) is immune to
	// wall-clock changes.
	start time.Time
	// startWall is the same instant with the monotonic reading stripped
	// (Round(0)), giving a pure wall-clock reference to compare against.
	startWall time.Time

	anchors []Anchor
	// lastDelta is the most recent (wall elapsed - mono elapsed), used to
	// detect a step as a *change* in the relationship rather than accumulated
	// drift since session start.
	lastDelta time.Duration
	lastTick  time.Time
}

// NewClock starts a session clock and records the opening anchor.
func NewClock() *Clock {
	now := time.Now()
	c := &Clock{
		start:     now,
		startWall: now.Round(0),
	}
	c.lastTick = now
	c.anchors = append(c.anchors, Anchor{
		Wall: c.startWall.UTC(),
		Mono: 0,
		Kind: AnchorStart,
	})
	return c
}

// Now returns the current wall-clock time and nanoseconds since session start.
// Every record in the index carries both (FR-003).
func (c *Clock) Now() (wall time.Time, mono int64) {
	return time.Now().Round(0).UTC(), int64(time.Since(c.start))
}

// Mono returns nanoseconds since session start.
func (c *Clock) Mono() int64 { return int64(time.Since(c.start)) }

// Start returns the session start in wall-clock terms.
func (c *Clock) Start() time.Time { return c.startWall.UTC() }

// Tick records a periodic anchor if one is due, and a step anchor if the wall
// clock has jumped relative to monotonic. Safe to call frequently — it decides
// for itself whether anything needs recording. Returns true if a step was
// detected, so the caller can surface it.
func (c *Clock) Tick() (stepped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	mono := int64(time.Since(c.start))
	wall := now.Round(0)

	// Wall elapsed vs monotonic elapsed. These track each other unless the
	// wall clock is moved.
	delta := wall.Sub(c.startWall) - time.Duration(mono)

	if d := delta - c.lastDelta; d > StepThreshold || d < -StepThreshold {
		c.anchors = append(c.anchors, Anchor{Wall: wall.UTC(), Mono: mono, Kind: AnchorStep})
		c.lastDelta = delta
		c.lastTick = now
		return true
	}
	c.lastDelta = delta

	if time.Since(c.lastTick) >= AnchorInterval {
		c.anchors = append(c.anchors, Anchor{Wall: wall.UTC(), Mono: mono, Kind: AnchorPeriodic})
		c.lastTick = now
	}
	return false
}

// Close records the final anchor. Idempotent.
func (c *Clock) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := len(c.anchors); n > 0 && c.anchors[n-1].Kind == AnchorEnd {
		return
	}
	wall, mono := time.Now().Round(0).UTC(), int64(time.Since(c.start))
	c.anchors = append(c.anchors, Anchor{Wall: wall, Mono: mono, Kind: AnchorEnd})
}

// Anchors returns a copy of the recorded anchors, for writing into session.json.
func (c *Clock) Anchors() []Anchor {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Anchor, len(c.anchors))
	copy(out, c.anchors)
	return out
}
