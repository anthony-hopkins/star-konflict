// Package annotate records labelled moments on a session's timeline.
//
// A marker is written to markers.log AND broadcast as a UDP datagram, so it
// also lands inline in the pcapng, timestamped by the same capture clock as the
// game's own traffic. That gives a rigid binding between a screen recording, a
// log line and a frame number, with no clock-skew assumptions anywhere — the
// design is inherited from the existing capture manual's beacon, and it is good
// enough to be worth keeping rather than reinventing.
package annotate

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MarkersFile is the conventional name, matching the capture manual.
const MarkersFile = "markers.log"

// DefaultBeaconPort is the UDP port the beacon broadcasts on.
const DefaultBeaconPort = 24242

// Marker kinds.
const (
	KindHeartbeat = "HEARTBEAT"
	KindEvent     = "EVENT"
	KindDerived   = "DERIVED"
	KindAnomaly   = "ANOMALY"
)

// Marker is one labelled point on the timeline.
type Marker struct {
	Seq   int
	Wall  time.Time
	Mono  int64
	Kind  string
	Label string
}

// Format renders the wire and log representation:
//
//	SCMARK|000042|2026-08-14T20:31:40.118Z|884421993310|EVENT|label
func (m Marker) Format() string {
	label := strings.ReplaceAll(m.Label, "\n", " ")
	return fmt.Sprintf("SCMARK|%06d|%s|%d|%s|%s",
		m.Seq, m.Wall.UTC().Format("2006-01-02T15:04:05.000Z"), m.Mono, m.Kind, label)
}

// Parse reads a marker line back.
func Parse(line string) (Marker, error) {
	parts := strings.SplitN(strings.TrimSpace(line), "|", 6)
	if len(parts) < 6 || parts[0] != "SCMARK" {
		return Marker{}, fmt.Errorf("not a marker line")
	}
	seq, err := strconv.Atoi(parts[1])
	if err != nil {
		return Marker{}, fmt.Errorf("bad sequence %q", parts[1])
	}
	wall, err := time.Parse("2006-01-02T15:04:05.000Z", parts[2])
	if err != nil {
		return Marker{}, fmt.Errorf("bad timestamp %q", parts[2])
	}
	mono, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return Marker{}, fmt.Errorf("bad monotonic %q", parts[3])
	}
	return Marker{Seq: seq, Wall: wall, Mono: mono, Kind: parts[4], Label: parts[5]}, nil
}

// Beacon appends markers to a session's log and broadcasts them.
type Beacon struct {
	mu   sync.Mutex
	f    *os.File
	conn net.PacketConn
	dst  *net.UDPAddr
	seq  int
	port int
}

// OpenBeacon opens (or reopens) the marker log for a bundle.
//
// A failure to open the broadcast socket is not fatal: the log line is the
// durable record, and the datagram is an enrichment. Losing the datagram costs
// video synchronisation, not evidence.
func OpenBeacon(bundleDir string, port int) (*Beacon, error) {
	if port == 0 {
		port = DefaultBeaconPort
	}
	path := bundleDir + string(os.PathSeparator) + MarkersFile

	seq, err := lastSeq(path)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	b := &Beacon{f: f, seq: seq, port: port}
	b.conn, b.dst = openBroadcast(port)
	return b, nil
}

func lastSeq(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	last := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m, err := Parse(sc.Text()); err == nil && m.Seq > last {
			last = m.Seq
		}
	}
	return last, sc.Err()
}

func openBroadcast(port int) (net.PacketConn, *net.UDPAddr) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			}); err != nil {
				return err
			}
			return serr
		},
	}
	conn, err := lc.ListenPacket(nil, "udp4", ":0")
	if err != nil {
		return nil, nil
	}
	return conn, &net.UDPAddr{IP: net.IPv4bcast, Port: port}
}

// Stamp records a marker. Returns what was recorded so a caller can echo it.
func (b *Beacon) Stamp(kind, label string, wall time.Time, mono int64) (Marker, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.seq++
	m := Marker{Seq: b.seq, Wall: wall, Mono: mono, Kind: kind, Label: label}
	line := m.Format()

	if _, err := fmt.Fprintln(b.f, line); err != nil {
		return m, err
	}
	// Markers are cheap and few; sync each one so a kill never loses the label
	// that explains what the surrounding traffic was.
	if err := b.f.Sync(); err != nil {
		return m, err
	}

	if b.conn != nil && b.dst != nil {
		// Best effort: the durable record is the log line.
		_ = b.conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = b.conn.WriteTo([]byte(line), b.dst)
	}
	return m, nil
}

// Count returns how many markers have been stamped.
func (b *Beacon) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Port returns the beacon's broadcast port, or 0 if broadcasting failed.
func (b *Beacon) Port() int {
	if b.conn == nil {
		return 0
	}
	return b.port
}

// Close releases the log and socket.
func (b *Beacon) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		_ = b.conn.Close()
	}
	if b.f != nil {
		return b.f.Close()
	}
	return nil
}
