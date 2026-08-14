package decode

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/sc-re/sc-capture/internal/decode/isolate"
	"github.com/sc-re/sc-capture/internal/flow"
	"github.com/sc-re/sc-capture/pkg/scproto"
)

// DecoderVersion stamps every result, so a re-decode years later can be
// compared against what was recorded at capture time.
const DecoderVersion = "1"

// Emit receives finished records. Implementations must not block for long: the
// pipeline runs behind capture, and while it can never cost a byte, a slow
// consumer costs records in the derived index.
type Emit func(Record)

// Pipeline turns frames into records.
//
// Not safe for concurrent Feed calls — drive it from one goroutine.
type Pipeline struct {
	table   *flow.Table
	emit    Emit
	seq     uint64
	streams map[streamKey]*stream

	mu        sync.Mutex
	observed  map[string]bool // element key -> seen, for coverage
	novel     []scproto.Element
	credSeen  bool
	desyncs   int
	failures  int
	handoffs  []Handoff
	recordCnt uint64
}

// Handoff is a dedicated-server announcement seen in the traffic.
type Handoff struct {
	Addr      string    `json:"addr"`
	Port      uint16    `json:"port"`
	SessionID uint64    `json:"session_id"`
	ZoneID    int32     `json:"zone_id"`
	At        time.Time `json:"at"`
}

type streamKey struct {
	conn string
	dir  flow.Direction
}

// stream is one direction of one TCP connection.
type stream struct {
	scanner *scproto.Scanner
	nextSeq uint32
	started bool
	// ledger maps buffered bytes back to the frames that carried them, so a
	// record can cite exactly the frames its bytes came from even when a
	// message spans several.
	ledger []contribution
}

type contribution struct {
	ref FrameRef
	n   int
}

// New returns a pipeline writing records to emit.
func New(table *flow.Table, emit Emit) *Pipeline {
	return &Pipeline{
		table:    table,
		emit:     emit,
		streams:  map[streamKey]*stream{},
		observed: map[string]bool{},
	}
}

// Feed processes one journaled frame.
//
// Any error here is contained and reported; it never propagates to the caller
// as a reason to stop capturing.
func (p *Pipeline) Feed(ref FrameRef, data []byte, wall time.Time, mono int64) {
	_ = isolate.Run(func() error {
		p.feed(ref, data, wall, mono)
		return nil
	})
}

func (p *Pipeline) feed(ref FrameRef, data []byte, wall time.Time, mono int64) {
	pkt := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.DecodeOptions{
		Lazy:   true,
		NoCopy: true,
	})

	netLayer := pkt.NetworkLayer()
	if netLayer == nil {
		return // ARP, LLDP and similar: journaled, not interpreted
	}
	src, dst := netLayer.NetworkFlow().Endpoints()
	srcIP := net.ParseIP(src.String())
	dstIP := net.ParseIP(dst.String())
	if srcIP == nil || dstIP == nil {
		return
	}

	switch t := pkt.TransportLayer().(type) {
	case *layers.TCP:
		conn, dir := p.table.Observe(srcIP, uint16(t.SrcPort), dstIP, uint16(t.DstPort), false, wall)
		p.feedTCP(conn, dir, t, ref, wall, mono)
	case *layers.UDP:
		conn, dir := p.table.Observe(srcIP, uint16(t.SrcPort), dstIP, uint16(t.DstPort), true, wall)
		p.feedUDP(conn, dir, t, ref, wall, mono)
	}
}

// feedUDP records each datagram as one record.
//
// The in-match protocol is not framed the way the master-server protocol is, so
// there is nothing to reassemble and — deliberately — nothing to guess at. A
// datagram is recorded as an undecoded unit with its bytes cited. That is the
// honest state of knowledge: nothing has ever decoded this traffic.
func (p *Pipeline) feedUDP(conn *flow.Connection, dir flow.Direction, t *layers.UDP, ref FrameRef, wall time.Time, mono int64) {
	payload := t.Payload
	if len(payload) == 0 {
		return
	}

	res := &Result{
		Status:         StatusUndecoded,
		Reason:         "datagram protocol; no decoder has ever been written for this traffic",
		DecoderVersion: DecoderVersion,
	}
	if conn.Service == flow.ServiceDedicated {
		res.Reason = "in-match dedicated-server traffic; layout unknown"
	}
	p.emitRecord(conn, dir, wall, mono, []FrameRef{ref}, len(payload), res)
}

func (p *Pipeline) feedTCP(conn *flow.Connection, dir flow.Direction, t *layers.TCP, ref FrameRef, wall time.Time, mono int64) {
	payload := t.Payload
	if len(payload) == 0 {
		return
	}

	key := streamKey{conn: conn.ConnID, dir: dir}
	s, ok := p.streams[key]
	if !ok {
		s = &stream{scanner: scproto.NewScanner()}
		p.streams[key] = s
	}
	if s.scanner.Desynced() {
		return // bytes are journaled; meaning is unavailable on this stream
	}

	seq := uint32(t.Seq)
	switch {
	case !s.started:
		s.started = true
		s.nextSeq = seq
	case seq == s.nextSeq:
		// in order
	case seqLess(seq, s.nextSeq):
		// Retransmission or overlap: skip the bytes already consumed.
		skip := int(s.nextSeq - seq)
		if skip >= len(payload) {
			return // wholly duplicate
		}
		payload = payload[skip:]
		seq = s.nextSeq
	default:
		// A gap. We do not buffer out of order and we do not guess: framing is
		// lost, so decoding degrades to "bytes stored, meaning unknown" while
		// capture continues untouched.
		p.table.MarkDesync(conn, ref.Index)
		p.mu.Lock()
		p.desyncs++
		p.mu.Unlock()
		s.scanner.Feed(nil)
		p.markStreamDesynced(s)
		return
	}

	s.nextSeq = seq + uint32(len(payload))
	s.scanner.Feed(payload)
	s.ledger = append(s.ledger, contribution{ref: ref, n: len(payload)})

	for {
		msg, ok, err := s.scanner.Next()
		if err != nil {
			p.table.MarkDesync(conn, ref.Index)
			p.mu.Lock()
			p.desyncs++
			p.mu.Unlock()
			return
		}
		if !ok {
			return
		}
		frames := s.take(len(msg.Raw))
		p.interpret(conn, dir, msg, wall, mono, frames)
	}
}

// markStreamDesynced forces the scanner into the stopped state after a
// transport-level gap, so it cannot resynchronise on a coincidence.
func (p *Pipeline) markStreamDesynced(s *stream) {
	s.scanner.Feed([]byte{0xde, 0xad, 0xbe, 0xef, 0, 0, 0, 0, 0, 0, 0, 0})
	_, _, _ = s.scanner.Next()
}

// take pops the frames that carried the next n buffered bytes.
func (s *stream) take(n int) []FrameRef {
	var out []FrameRef
	seen := map[FrameRef]bool{}
	for n > 0 && len(s.ledger) > 0 {
		c := &s.ledger[0]
		if !seen[c.ref] {
			out = append(out, c.ref)
			seen[c.ref] = true
		}
		if c.n > n {
			c.n -= n
			n = 0
			break
		}
		n -= c.n
		s.ledger = s.ledger[1:]
	}
	if len(out) == 0 {
		// Should not happen, but a record must always cite its evidence; an
		// uncited record is a bug, not a record.
		out = []FrameRef{{Segment: "", Index: 0}}
	}
	return out
}

// interpret names a message and records what it is.
func (p *Pipeline) interpret(conn *flow.Connection, dir flow.Direction, msg scproto.Message, wall time.Time, mono int64, frames []FrameRef) {
	res := &Result{DecoderVersion: DecoderVersion}

	if msg.Header.Special {
		res.Status = StatusDecoded
		res.MessageType = "SPECIAL_FRAME"
		res.Fields = map[string]any{"sentinel": fmt.Sprintf("%#08x", msg.Header.Length)}
		p.emitRecord(conn, dir, wall, mono, frames, len(msg.Raw), res)
		return
	}

	mt := msg.Header.Type
	id := uint16(mt)
	res.MessageTypeID = &id
	res.MessageType = mt.String()
	res.ElementKind = KindMessageType

	if !mt.Known() {
		res.Status = StatusUnknownElement
		res.Novel = true
		res.Reason = "message type absent from the known universe"
		p.noteNovel(scproto.Element{Kind: scproto.KindMessageType, ID: id, Name: res.MessageType})
	} else {
		p.observe(scproto.KindMessageType, id, false)
		res.Status = StatusUndecoded
		res.Reason = "body layout not known for this message type"
	}

	if mt.CarriesCredentials() {
		p.mu.Lock()
		p.credSeen = true
		p.mu.Unlock()
	}

	// Containers: the inner opcode is the element that actually matters for
	// coverage, so name it even when its body is opaque.
	switch mt {
	case scproto.CSCMDAsyncReq:
		if op, ok := msg.AsyncRequestType(); ok {
			oid := uint16(op)
			res.Element = op.String()
			res.ElementID = &oid
			res.ElementKind = KindAsyncRequest
			if op.Known() {
				p.observe(scproto.KindAsyncRequest, oid, false)
				res.Status = StatusUndecoded
				res.Reason = "opcode known, body layout not decoded"
				if op == scproto.ACPlayerCredentials {
					p.mu.Lock()
					p.credSeen = true
					p.mu.Unlock()
				}
			} else {
				res.Status = StatusUnknownElement
				res.Novel = true
				res.Reason = "async-request opcode absent from the known universe"
				p.noteNovel(scproto.Element{Kind: scproto.KindAsyncRequest, ID: oid, Name: res.Element})
			}
		}

	case scproto.SCMDNotification:
		if nt, ok := msg.NotificationType(); ok {
			nid := uint16(nt)
			res.Element = nt.String()
			res.ElementID = &nid
			res.ElementKind = KindNotification
			if nt.Known() {
				p.observe(scproto.KindNotification, nid, false)
				res.Status = StatusUndecoded
				res.Reason = "notification type known, body layout not decoded"
			} else {
				res.Status = StatusUnknownElement
				res.Novel = true
				res.Reason = "notification type absent from the known universe"
				p.noteNovel(scproto.Element{Kind: scproto.KindNotification, ID: nid, Name: res.Element})
			}
		}

	case scproto.SCMDConnectDedicatedServer:
		// The handoff is the pointer to the in-match protocol. Decoding it is
		// what lets the UDP flow that follows be classified from the protocol
		// rather than guessed from a port.
		if err := isolate.Run(func() error {
			ds, err := scproto.ParseDedicatedServer(msg.Body)
			if err != nil && ds == nil {
				return err
			}
			if ds.Addr != "" {
				p.table.AnnounceDedicated(ds.Addr, ds.Port, ds.SessionID)
				p.mu.Lock()
				p.handoffs = append(p.handoffs, Handoff{
					Addr: ds.Addr, Port: ds.Port, SessionID: ds.SessionID,
					ZoneID: ds.ZoneID, At: wall.UTC(),
				})
				p.mu.Unlock()
			}
			res.Status = StatusDecoded
			res.Fields = map[string]any{
				"addr": ds.Addr, "port": ds.Port,
				"session_id": ds.SessionID, "zone_id": ds.ZoneID,
			}
			if err != nil {
				res.Reason = err.Error()
			}
			p.observe(scproto.KindMessageType, id, true)
			return nil
		}); err != nil {
			res.Status = StatusFailed
			res.Reason = err.Error()
			p.mu.Lock()
			p.failures++
			p.mu.Unlock()
		}

	case scproto.SCMDAssignedShard:
		if err := isolate.Run(func() error {
			as, err := scproto.ParseAssignedShard(msg.Body)
			if err != nil && as == nil {
				return err
			}
			fields := map[string]any{}
			var shards []string
			for _, e := range as.Shards {
				shards = append(shards, fmt.Sprintf("%s:%d", e.IP, e.Port))
			}
			fields["shards"] = shards
			if as.Chat != nil {
				fields["chat"] = fmt.Sprintf("%s:%d", as.Chat.IP, as.Chat.Port)
			}
			res.Status = StatusDecoded
			res.Fields = fields
			if err != nil {
				res.Reason = err.Error()
			}
			p.observe(scproto.KindMessageType, id, true)
			return nil
		}); err != nil {
			res.Status = StatusFailed
			res.Reason = err.Error()
			p.mu.Lock()
			p.failures++
			p.mu.Unlock()
		}
	}

	p.emitRecord(conn, dir, wall, mono, frames, len(msg.Raw), res)
}

func (p *Pipeline) emitRecord(conn *flow.Connection, dir flow.Direction, wall time.Time, mono int64, frames []FrameRef, n int, res *Result) {
	p.mu.Lock()
	p.seq++
	seq := p.seq
	p.recordCnt++
	p.mu.Unlock()

	if p.emit != nil {
		p.emit(Record{
			Seq: seq, ConnID: conn.ConnID, Dir: string(dir),
			TWall: wall.UTC(), TMono: mono,
			Frames: frames, ByteLen: n, Decode: res,
		})
	}
}

// observe records that an element has been seen, and whether it decoded.
func (p *Pipeline) observe(kind scproto.Kind, id uint16, decoded bool) {
	key := string(kind) + ":" + fmt.Sprint(id)
	if decoded {
		key += "!"
	}
	p.mu.Lock()
	p.observed[key] = true
	p.mu.Unlock()
}

func (p *Pipeline) noteNovel(e scproto.Element) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.novel {
		if n.Kind == e.Kind && n.ID == e.ID {
			return
		}
	}
	p.novel = append(p.novel, e)
}

// Stats reports what the pipeline has seen.
type Stats struct {
	Records           uint64
	Desyncs           int
	Failures          int
	Novel             []scproto.Element
	CredentialsSeen   bool
	Handoffs          []Handoff
	ObservedElements  map[string]bool
	DistinctObserved  int
	DistinctDecodedOK int
}

// Stats returns a snapshot.
func (p *Pipeline) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()

	obs := make(map[string]bool, len(p.observed))
	decoded := 0
	for k, v := range p.observed {
		obs[k] = v
		if len(k) > 0 && k[len(k)-1] == '!' {
			decoded++
		}
	}
	novel := make([]scproto.Element, len(p.novel))
	copy(novel, p.novel)
	handoffs := make([]Handoff, len(p.handoffs))
	copy(handoffs, p.handoffs)

	return Stats{
		Records: p.recordCnt, Desyncs: p.desyncs, Failures: p.failures,
		Novel: novel, CredentialsSeen: p.credSeen, Handoffs: handoffs,
		ObservedElements: obs, DistinctObserved: len(obs) - decoded,
		DistinctDecodedOK: decoded,
	}
}

// seqLess compares TCP sequence numbers with wraparound.
func seqLess(a, b uint32) bool { return int32(a-b) < 0 }
