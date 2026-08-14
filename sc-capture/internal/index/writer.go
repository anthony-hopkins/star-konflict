// Package index writes the derived record index.
//
// index.jsonl is DERIVED DATA and says so everywhere it can. It is append-only
// JSON Lines because that shape has exactly the right failure mode: an abrupt
// kill truncates at most the final line, and the truncation is detectable by a
// failed parse of that one line. A session is valid without it, and
// `sccap index --rebuild` regenerates it from the pcapng segments alone.
package index

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sc-re/sc-capture/internal/decode"
)

// File is the conventional name inside a bundle.
const File = "index.jsonl"

// FlushInterval matches the journal's, so index and journal lose the same
// amount on an abrupt termination rather than disagreeing about what happened.
const FlushInterval = time.Second

// Writer appends records to index.jsonl.
type Writer struct {
	mu        sync.Mutex
	f         *os.File
	bw        *bufio.Writer
	enc       *json.Encoder
	lastFlush time.Time
	written   uint64
	dropped   uint64
}

// Create opens (or truncates) the index in a bundle.
func Create(bundleDir string) (*Writer, error) {
	f, err := os.OpenFile(filepath.Join(bundleDir, File),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriterSize(f, 1<<18)
	enc := json.NewEncoder(bw)
	return &Writer{f: f, bw: bw, enc: enc, lastFlush: time.Now()}, nil
}

// Write appends one record.
func (w *Writer) Write(r decode.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(r); err != nil {
		return err
	}
	w.written++
	if time.Since(w.lastFlush) >= FlushInterval {
		return w.syncLocked()
	}
	return nil
}

// Tick flushes on the interval even when records are sparse.
func (w *Writer) Tick() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.lastFlush) < FlushInterval {
		return nil
	}
	return w.syncLocked()
}

func (w *Writer) syncLocked() error {
	if err := w.bw.Flush(); err != nil {
		return err
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	w.lastFlush = time.Now()
	return nil
}

// Dropped records that a record was discarded because decode fell behind.
//
// Losing a record from the index costs nothing permanent — the bytes are in the
// journal and the index can be rebuilt — but it must be counted, because a
// silently short index would read as "nothing else happened".
func (w *Writer) Dropped() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dropped++
}

// Counts returns records written and dropped.
func (w *Writer) Counts() (written, dropped uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written, w.dropped
}

// Close flushes and closes.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.bw.Flush(); err != nil {
		return err
	}
	if err := w.f.Sync(); err != nil {
		return err
	}
	return w.f.Close()
}
