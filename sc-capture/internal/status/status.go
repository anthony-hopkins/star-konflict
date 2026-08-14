// Package status renders the live progress line.
//
// Everything here goes to stderr, so stdout stays clean for piped data
// (contracts/cli.md). There is deliberately no TUI: this tool runs alongside a
// game and must not compete for the terminal or add a dependency.
package status

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Counters holds the live capture numbers. Updated from the capture path with
// atomics so rendering never blocks a writer.
type Counters struct {
	Frames  atomic.Uint64
	Bytes   atomic.Uint64
	Drops   atomic.Uint64
	Records atomic.Uint64
	Novel   atomic.Uint64
}

// Reporter renders one status line per second and prints persistent lines for
// events worth keeping in the scrollback.
type Reporter struct {
	w     io.Writer
	c     *Counters
	start time.Time

	mu       sync.Mutex
	services map[string]struct{}
	tty      bool
	stop     chan struct{}
	done     chan struct{}
	lastLen  int
}

func New(w io.Writer, c *Counters, tty bool) *Reporter {
	return &Reporter{
		w:        w,
		c:        c,
		start:    time.Now(),
		services: make(map[string]struct{}),
		tty:      tty,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start begins the 1 Hz refresh.
func (r *Reporter) Start() {
	go func() {
		defer close(r.done)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.stop:
				r.render()
				fmt.Fprintln(r.w)
				return
			case <-t.C:
				r.render()
			}
		}
	}()
}

// Stop ends the refresh and leaves the final line on screen.
func (r *Reporter) Stop() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	<-r.done
}

// Service records that a logical service has been seen this session.
func (r *Reporter) Service(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[name] = struct{}{}
}

// Notef prints a line that stays in the scrollback, above the status line.
// Used for things a contributor must not miss — a novel element, a warning.
func (r *Reporter) Notef(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tty && r.lastLen > 0 {
		fmt.Fprintf(r.w, "\r%s\r", strings.Repeat(" ", r.lastLen))
		r.lastLen = 0
	}
	fmt.Fprintf(r.w, format+"\n", args...)
}

func (r *Reporter) render() {
	r.mu.Lock()
	defer r.mu.Unlock()

	svc := make([]string, 0, len(r.services))
	for s := range r.services {
		svc = append(svc, s)
	}
	sort.Strings(svc)
	svcs := strings.Join(svc, ",")
	if svcs == "" {
		svcs = "-"
	}

	d := time.Since(r.start).Truncate(time.Second)
	line := fmt.Sprintf("[%02d:%02d:%02d] services=%s  frames=%d  journal=%s  drops=%d  records=%d  novel=%d",
		int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60,
		svcs,
		r.c.Frames.Load(),
		humanBytes(r.c.Bytes.Load()),
		r.c.Drops.Load(),
		r.c.Records.Load(),
		r.c.Novel.Load(),
	)

	if r.tty {
		pad := ""
		if n := r.lastLen - len(line); n > 0 {
			pad = strings.Repeat(" ", n)
		}
		fmt.Fprintf(r.w, "\r%s%s", line, pad)
		r.lastLen = len(line)
		return
	}
	fmt.Fprintln(r.w, line)
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%ciB", float64(b)/float64(div), "KMGTP"[exp])
}
