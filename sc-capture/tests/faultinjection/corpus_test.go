// Package faultinjection is required by the constitution's development
// workflow: "Fault injection is a required test ... the suite MUST include
// decoder failure, malformed frames and stream desync, and MUST assert that
// byte capture survives all of them."
//
// Principle II is only real if it is tested. These tests build synthetic
// sessions containing each failure mode, replay them, and assert that the raw
// journal is untouched, no panic escapes, and the offending record is present
// and honestly labelled rather than silently missing.
package faultinjection

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/sc-re/sc-capture/internal/decode"
	"github.com/sc-re/sc-capture/internal/decode/isolate"
	"github.com/sc-re/sc-capture/internal/flow"
	"github.com/sc-re/sc-capture/pkg/scproto"
)

const (
	clientIP   = "10.0.0.2"
	serverIP   = "10.0.0.1"
	clientPort = 50000
	shardPort  = 3802
)

// segmentSpec is one TCP segment to synthesise.
type segmentSpec struct {
	fromServer bool
	seq        uint32
	payload    []byte
}

// buildSession writes a synthetic pcapng containing the given TCP segments.
func buildSession(t *testing.T, dir string, segs []segmentSpec) string {
	t.Helper()

	path := filepath.Join(dir, "capture_00001_20260101000000.pcapng")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()

	w, err := pcapgo.NewNgWriterInterface(f, pcapgo.NgInterface{
		Name:                "test0",
		LinkType:            layers.LinkTypeEthernet,
		TimestampResolution: 9,
	}, pcapgo.NgWriterOptions{})
	if err != nil {
		t.Fatalf("pcapng writer: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, s := range segs {
		raw := buildTCPFrame(t, s)
		ci := gopacket.CaptureInfo{
			Timestamp:     base.Add(time.Duration(i) * time.Millisecond),
			CaptureLength: len(raw),
			Length:        len(raw),
		}
		if err := w.WritePacket(ci, raw); err != nil {
			t.Fatalf("write packet: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return path
}

func buildTCPFrame(t *testing.T, s segmentSpec) []byte {
	t.Helper()

	src, dst := clientIP, serverIP
	sport, dport := uint16(clientPort), uint16(shardPort)
	if s.fromServer {
		src, dst = serverIP, clientIP
		sport, dport = shardPort, clientPort
	}

	eth := &layers.Ethernet{
		SrcMAC: []byte{0, 0, 0, 0, 0, 1}, DstMAC: []byte{0, 0, 0, 0, 0, 2},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolTCP,
		SrcIP: parseIP(src), DstIP: parseIP(dst),
	}
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(sport), DstPort: layers.TCPPort(dport),
		Seq: s.seq, ACK: true, Window: 65535,
	}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("checksum layer: %v", err)
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, eth, ip, tcp,
		gopacket.Payload(s.payload)); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return buf.Bytes()
}

func parseIP(s string) []byte {
	var b [4]byte
	var n, cur int
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			b[n] = byte(cur)
			n++
			cur = 0
			continue
		}
		cur = cur*10 + int(s[i]-'0')
	}
	return b[:]
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// replay runs the decoder over a bundle and returns what it produced.
func replay(t *testing.T, dir string) ([]decode.Record, decode.Stats, *flow.Table) {
	t.Helper()
	var records []decode.Record
	table, stats, err := decode.Replay(dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		func(r decode.Record) { records = append(records, r) })
	if err != nil {
		t.Fatalf("replay returned an error, which must never abort a session: %v", err)
	}
	return records, stats, table
}

// TestControlRunDecodesCleanly establishes the baseline the fault cases are
// measured against.
func TestControlRunDecodesCleanly(t *testing.T) {
	dir := t.TempDir()
	var seq uint32 = 1000
	var segs []segmentSpec
	for i := 0; i < 4; i++ {
		msg := scproto.Build(scproto.CSCMDAsyncReq, uint16(i), 0,
			[]byte{0x00, 0x09, 0xaa, 0xbb})
		segs = append(segs, segmentSpec{seq: seq, payload: msg})
		seq += uint32(len(msg))
	}
	buildSession(t, dir, segs)

	records, stats, _ := replay(t, dir)
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}
	for _, r := range records {
		if r.Decode.Element != "AC_PLAYER_CREDENTIALS" {
			t.Errorf("element = %q, want AC_PLAYER_CREDENTIALS", r.Decode.Element)
		}
		if len(r.Frames) == 0 {
			t.Error("record cites no frames; an uncited record is a bug, not a record")
		}
	}
	if !stats.CredentialsSeen {
		t.Error("AC_PLAYER_CREDENTIALS observed but the credential warning was not raised")
	}
	if stats.Desyncs != 0 || stats.Failures != 0 {
		t.Errorf("control run reported %d desyncs and %d failures", stats.Desyncs, stats.Failures)
	}
}

// TestMalformedFrameDoesNotLoseBytes covers a corrupt frame mid-stream.
func TestMalformedFrameDoesNotLoseBytes(t *testing.T) {
	dir := t.TempDir()

	good := scproto.Build(scproto.CSCMDAsyncReq, 1, 0, []byte{0x00, 0x09})
	corrupt := append([]byte{}, scproto.Build(scproto.CSCMDAsyncReq, 2, 0, []byte{0x00, 0x09})...)
	corrupt[11] ^= 0xff // damage the checksum

	var seq uint32 = 500
	segs := []segmentSpec{
		{seq: seq, payload: good},
		{seq: seq + uint32(len(good)), payload: corrupt},
	}
	path := buildSession(t, dir, segs)
	before := hashFile(t, path)

	records, stats, table := replay(t, dir)

	if hashFile(t, path) != before {
		t.Fatal("BYTE LOSS: replaying a malformed frame modified the raw journal")
	}
	if len(records) == 0 {
		t.Error("the good message before the corruption was lost")
	}
	if stats.Desyncs == 0 {
		t.Error("a corrupt frame did not register as a desync; degradation must be visible")
	}
	var desynced bool
	for _, c := range table.Connections() {
		if c.ReassemblyState == "desynced" && c.DesyncAtFrame != nil {
			desynced = true
		}
	}
	if !desynced {
		t.Error("the desync point was not recorded on the connection")
	}
}

// TestStreamDesyncDegradesRatherThanGuessing covers a gap in the TCP stream:
// framing is lost, so decoding must stop rather than emit confident nonsense.
func TestStreamDesyncDegradesRatherThanGuessing(t *testing.T) {
	dir := t.TempDir()

	first := scproto.Build(scproto.CSCMDAsyncReq, 1, 0, []byte{0x00, 0x09})
	later := scproto.Build(scproto.CSCMDAsyncReq, 2, 0, []byte{0x00, 0x09})

	var seq uint32 = 700
	segs := []segmentSpec{
		{seq: seq, payload: first},
		// Deliberately skip ahead: bytes that never arrived.
		{seq: seq + uint32(len(first)) + 500, payload: later},
	}
	path := buildSession(t, dir, segs)
	before := hashFile(t, path)

	records, stats, _ := replay(t, dir)

	if hashFile(t, path) != before {
		t.Fatal("BYTE LOSS: a stream gap modified the raw journal")
	}
	if stats.Desyncs == 0 {
		t.Error("a sequence gap was not reported as a desync")
	}
	if len(records) != 1 {
		t.Errorf("got %d records, want 1 (only the message before the gap)", len(records))
	}
}

// TestUnknownElementsAreFlaggedNotDropped covers an opcode and a message type
// that this build has never heard of. Both are discoveries.
func TestUnknownElementsAreFlaggedNotDropped(t *testing.T) {
	dir := t.TempDir()

	unknownType := scproto.Build(scproto.MessageType(200), 1, 0, []byte{0x01})
	unknownOpcode := scproto.Build(scproto.CSCMDAsyncReq, 2, 0, []byte{0xff, 0xfe})

	var seq uint32 = 900
	segs := []segmentSpec{
		{seq: seq, payload: unknownType},
		{seq: seq + uint32(len(unknownType)), payload: unknownOpcode},
	}
	path := buildSession(t, dir, segs)
	before := hashFile(t, path)

	records, stats, _ := replay(t, dir)

	if hashFile(t, path) != before {
		t.Fatal("BYTE LOSS: unknown elements modified the raw journal")
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 — unknown elements must be recorded, not dropped",
			len(records))
	}
	for _, r := range records {
		if r.Decode.Status != decode.StatusUnknownElement {
			t.Errorf("record %d status = %q, want %q",
				r.Seq, r.Decode.Status, decode.StatusUnknownElement)
		}
		if !r.Decode.Novel {
			t.Errorf("record %d not flagged novel", r.Seq)
		}
	}
	if len(stats.Novel) != 2 {
		t.Errorf("stats report %d novel elements, want 2", len(stats.Novel))
	}
}

// TestMessageSpanningSegmentsCitesEveryFrame: a message split across TCP
// segments must still point at all the evidence it came from.
func TestMessageSpanningSegments(t *testing.T) {
	dir := t.TempDir()

	msg := scproto.Build(scproto.CSCMDAsyncReq, 1, 0, make([]byte, 300))
	var seq uint32 = 100
	segs := []segmentSpec{
		{seq: seq, payload: msg[:50]},
		{seq: seq + 50, payload: msg[50:200]},
		{seq: seq + 200, payload: msg[200:]},
	}
	buildSession(t, dir, segs)

	records, _, _ := replay(t, dir)
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1 reassembled message", len(records))
	}
	if n := len(records[0].Frames); n != 3 {
		t.Errorf("record cites %d frames, want 3 — a record must cite every frame "+
			"its bytes came from", n)
	}
	if records[0].ByteLen != len(msg) {
		t.Errorf("byte_len = %d, want %d", records[0].ByteLen, len(msg))
	}
}

// TestDedicatedHandoffIsDetected covers the message that points at the in-match
// protocol — the single irreplaceable gap this project exists to close.
func TestDedicatedHandoffIsDetected(t *testing.T) {
	dir := t.TempDir()

	// cstring addr, u16 port, u64 session, i32 zone, 1 bit flag
	body := append([]byte("192.0.2.55\x00"),
		0x75, 0x30, // port 30000
		0, 0, 0, 0, 0, 0, 0x04, 0xd2, // session 1234
		0, 0, 0, 0x07, // zone 7
		0x80, // flag bit
	)
	msg := scproto.Build(scproto.SCMDConnectDedicatedServer, 1, 0, body)
	buildSession(t, dir, []segmentSpec{{fromServer: true, seq: 10, payload: msg}})

	records, stats, _ := replay(t, dir)
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	r := records[0]
	if r.Decode.Status != decode.StatusDecoded {
		t.Fatalf("handoff status = %q (%s), want decoded", r.Decode.Status, r.Decode.Reason)
	}
	if got := r.Decode.Fields["addr"]; got != "192.0.2.55" {
		t.Errorf("addr = %v, want 192.0.2.55", got)
	}
	if len(stats.Handoffs) != 1 {
		t.Fatalf("%d handoffs recorded, want 1", len(stats.Handoffs))
	}
	h := stats.Handoffs[0]
	if h.Port != 30000 || h.SessionID != 1234 || h.ZoneID != 7 {
		t.Errorf("handoff = %+v, want port 30000 session 1234 zone 7", h)
	}
}

// TestPanicsAreContained covers the constitution's "decoder failure" case
// directly: a panicking decoder must become a recorded failure on one record
// and nothing more.
func TestPanicsAreContained(t *testing.T) {
	err := isolate.Run(func() error {
		var p *int
		_ = *p // nil dereference
		return nil
	})
	if err == nil {
		t.Fatal("a panicking decoder was not contained")
	}
	rec, ok := err.(*isolate.Recovered)
	if !ok {
		t.Fatalf("error type = %T, want *isolate.Recovered", err)
	}
	if len(rec.Stack) == 0 {
		// The record that caused a panic is in the journal and can be replayed
		// against a fixed decoder later — but only if the stack survives to
		// say what broke.
		t.Error("no stack captured; a contained panic is a bug report and needs one")
	}
}

// TestGarbageFramesNeverPanic feeds structured noise straight at the pipeline.
// Nothing a hostile or corrupt wire can produce may take the process down,
// because taking the process down means the capture stops.
func TestGarbageFramesNeverPanic(t *testing.T) {
	table := flow.NewTable()
	p := decode.New(table, func(decode.Record) {})

	rng := rand.New(rand.NewSource(1337))
	for i := 0; i < 2000; i++ {
		n := 1 + rng.Intn(200)
		buf := make([]byte, n)
		rng.Read(buf)
		// Half the time, make it look enough like Ethernet/IPv4 to get deeper
		// into the parsers before the nonsense starts.
		if i%2 == 0 && n > 34 {
			buf[12], buf[13] = 0x08, 0x00
			buf[14] = 0x45
			buf[23] = byte(6 + rng.Intn(2)*11) // TCP or UDP
		}
		p.Feed(decode.FrameRef{Segment: "synthetic", Index: uint64(i)},
			buf, time.Now(), int64(i))
	}
	// Reaching here without a panic is the assertion.
}
