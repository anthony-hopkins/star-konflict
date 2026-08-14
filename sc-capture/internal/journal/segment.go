package journal

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
)

// Rotation and durability policy.
//
// The size and duration caps match the existing capture manual's soft caps, so
// bundles from this tool and from the manual's original procedure segment the
// same way. The flush cadence is what bounds loss on abrupt termination.
const (
	MaxSegmentBytes    = 200 << 20 // 200 MB
	MaxSegmentDuration = 10 * time.Minute
	FlushInterval      = time.Second
	FlushBytes         = 4 << 20 // 4 MiB
)

// SegmentInfo records what a finished segment contained, for session.json.
type SegmentInfo struct {
	File         string     `json:"file"`
	FirstFrameAt *time.Time `json:"first_frame_utc"`
	LastFrameAt  *time.Time `json:"last_frame_utc"`
	Frames       uint64     `json:"frames"`
}

// Writer is the rotating pcapng journal for one session.
//
// All writes funnel through a single goroutine's calls here, so no locking is
// needed for ordering; the mutex only guards concurrent stat reads.
type Writer struct {
	dir     string
	ifaces  []InterfaceSpec
	appName string
	osName  string

	mu      sync.Mutex
	seg     *segment
	segNum  int
	segOpen time.Time
	first   *time.Time
	last    *time.Time

	segments []SegmentInfo

	totalFrames uint64
	totalBytes  uint64
	lastFlush   time.Time
}

// NewWriter opens the first segment in dir.
func NewWriter(dir string, ifaces []InterfaceSpec, appName, osName string) (*Writer, error) {
	w := &Writer{dir: dir, ifaces: ifaces, appName: appName, osName: osName}
	if err := w.rotate(time.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

func segmentName(n int, at time.Time) string {
	return fmt.Sprintf("capture_%05d_%s.pcapng", n, at.UTC().Format("20060102150405"))
}

func (w *Writer) rotate(now time.Time) error {
	if w.seg != nil {
		if err := w.finishSegment(); err != nil {
			return err
		}
	}
	w.segNum++
	path := filepath.Join(w.dir, segmentName(w.segNum, now))
	seg, err := openSegment(path, w.ifaces, w.appName, w.osName)
	if err != nil {
		return err
	}
	w.seg = seg
	w.segOpen = now
	w.first, w.last = nil, nil
	w.lastFlush = now
	return nil
}

// finishSegment closes the current segment and records what it held. The file
// is fsynced before the next one is opened, so a crash during rotation cannot
// leave the outgoing segment partially on disk.
func (w *Writer) finishSegment() error {
	if w.seg == nil {
		return nil
	}
	info := SegmentInfo{
		File:         filepath.Base(w.seg.path),
		FirstFrameAt: w.first,
		LastFrameAt:  w.last,
		Frames:       w.seg.frames,
	}
	if err := w.seg.close(); err != nil {
		return err
	}
	w.segments = append(w.segments, info)
	w.seg = nil
	return nil
}

// Write journals one frame and reports where it landed.
//
// The returned segment name and zero-based frame index are what a record in the
// derived index cites to point back at its evidence. References are
// (segment, index) rather than a number global to the session, so losing one
// segment does not invalidate references into the others.
func (w *Writer) Write(ifaceID int, ci gopacket.CaptureInfo, data []byte) (segment string, index uint64, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := ci.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	if w.seg.bytes >= MaxSegmentBytes || time.Since(w.segOpen) >= MaxSegmentDuration {
		if err := w.rotate(now); err != nil {
			return "", 0, err
		}
	}

	index = w.seg.frames
	segment = filepath.Base(w.seg.path)

	if err := w.seg.write(ifaceID, ci, data); err != nil {
		return "", 0, err
	}

	utc := now.UTC()
	if w.first == nil {
		f := utc
		w.first = &f
	}
	l := utc
	w.last = &l

	w.totalFrames++
	w.totalBytes += uint64(len(data))

	if w.seg.unflushed >= FlushBytes || time.Since(w.lastFlush) >= FlushInterval {
		if err := w.seg.sync(); err != nil {
			return "", 0, err
		}
		w.lastFlush = time.Now()
	}
	return segment, index, nil
}

// Tick syncs if the flush interval has elapsed. Called from a timer so a quiet
// link still gets its data to disk promptly.
func (w *Writer) Tick() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seg == nil || time.Since(w.lastFlush) < FlushInterval {
		return nil
	}
	if err := w.seg.sync(); err != nil {
		return err
	}
	w.lastFlush = time.Now()
	return nil
}

// Stats reports totals for the status line.
func (w *Writer) Stats() (frames, bytes uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.totalFrames, w.totalBytes
}

// Segments returns metadata for every segment written, including the open one.
func (w *Writer) Segments() []SegmentInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := append([]SegmentInfo{}, w.segments...)
	if w.seg != nil {
		out = append(out, SegmentInfo{
			File:         filepath.Base(w.seg.path),
			FirstFrameAt: w.first,
			LastFrameAt:  w.last,
			Frames:       w.seg.frames,
		})
	}
	return out
}

// Close finishes the open segment.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.finishSegment()
}
