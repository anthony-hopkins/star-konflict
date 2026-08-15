package scproto

import (
	"encoding/binary"
	"errors"
)

// HeaderSize is the fixed 12-byte big-endian frame header:
//
//	[4B body_len][2B seq][2B echo_seq][2B cmd_type][2B checksum]
const HeaderSize = 12

// specialLengthThreshold marks a header whose first field is not a length.
//
// This is the detail that desyncs a naive parser permanently. Two known special
// frames exist — 0xffffffff (disconnect, carrying a reason byte) and 0xfffffffe
// (keepalive) — and both are complete in 12 bytes with no body. Reading a body
// for them consumes the start of the next frame, after which every subsequent
// length is garbage and the stream never recovers.
const specialLengthThreshold = 0xfffffc

// Header is a parsed frame header.
type Header struct {
	Length   uint32
	Seq      uint16
	EchoSeq  uint16
	Type     MessageType
	Checksum uint16
	// Special marks a frame whose Length field is a sentinel rather than a
	// body length. Such frames are exactly HeaderSize bytes.
	Special bool
}

// Message is one framed protocol message.
type Message struct {
	Header Header
	// Body is the message payload, empty for special frames. It aliases the
	// scanner's buffer and is only valid until the next Next call.
	Body []byte
	// Raw is the complete on-wire message including the header.
	Raw []byte
}

// ChecksumValid recomputes the header checksum over the body.
//
// A mismatch means either damage in transit, a capture defect, or — most
// usefully — that our framing has desynced and we are looking at the middle of
// a message. It is the cheapest desync detector available.
func (m *Message) ChecksumValid() bool {
	if m.Header.Special {
		return true // sentinel frames carry no meaningful checksum
	}
	return Checksum(m.Header.Length, m.Header.Seq, m.Header.EchoSeq,
		uint16(m.Header.Type), m.Body) == m.Header.Checksum
}

// AsyncRequestType returns the AC_* opcode carried by a CSCMD_ASYNC_REQ, taken
// from the first two bytes of the body.
func (m *Message) AsyncRequestType() (AsyncReqType, bool) {
	if m.Header.Type != CSCMDAsyncReq || len(m.Body) < 2 {
		return 0, false
	}
	return AsyncReqType(binary.BigEndian.Uint16(m.Body[0:2])), true
}

// NotificationType returns the SN_* opcode carried by an SCMD_NOTIFICATION.
func (m *Message) NotificationType() (NotificationType, bool) {
	if m.Header.Type != SCMDNotification || len(m.Body) < 2 {
		return 0, false
	}
	// The SN_* opcode is carried little-endian here — the id is in the low
	// (first) body byte — unlike the big-endian async-request id above. Reading
	// it big-endian multiplied every notification id by 256, so none matched the
	// known SN_* table and every one was misreported as a novel element.
	return NotificationType(binary.LittleEndian.Uint16(m.Body[0:2])), true
}

// ErrDesync reports that the byte stream no longer looks like framed messages.
var ErrDesync = errors.New("stream desync: header checksum does not match")

// MaxBodyLength is the largest body the framing can express.
//
// It is not a policy we chose — it falls out of the sentinel encoding. Any
// length above specialLengthThreshold is a sentinel rather than a length, so a
// corrupt or hostile header cannot ask the scanner to buffer more than this.
// The bound is the protocol's own, which is why there is no separate guard.
const MaxBodyLength = specialLengthThreshold

// Scanner extracts messages from a byte stream that arrives in arbitrary
// chunks.
//
// It parses from a buffer rather than from a socket deliberately: capture reads
// bytes that are already durable in the journal, so decoding never has a live
// connection to lose. That is what makes decode failures free.
type Scanner struct {
	buf     []byte
	off     int
	desync  bool
	Checked bool // when true, a checksum mismatch reports a desync
}

// NewScanner returns a scanner that validates checksums.
func NewScanner() *Scanner { return &Scanner{Checked: true} }

// Feed appends more stream bytes.
func (s *Scanner) Feed(b []byte) {
	// Compact consumed bytes before growing, so a long-lived stream does not
	// accumulate its whole history.
	if s.off > 0 && s.off == len(s.buf) {
		s.buf = s.buf[:0]
		s.off = 0
	} else if s.off > 1<<20 {
		s.buf = append(s.buf[:0], s.buf[s.off:]...)
		s.off = 0
	}
	s.buf = append(s.buf, b...)
}

// Buffered reports how many unparsed bytes are held.
func (s *Scanner) Buffered() int { return len(s.buf) - s.off }

// Desynced reports whether framing has been lost on this stream.
//
// Once desynced the scanner stops emitting messages rather than guessing. The
// bytes remain in the journal untouched: this degrades to "bytes stored,
// meaning unknown", which is exactly what Principle II requires.
func (s *Scanner) Desynced() bool { return s.desync }

// Next returns the next complete message, or ok=false if more bytes are needed.
func (s *Scanner) Next() (Message, bool, error) {
	if s.desync {
		return Message{}, false, ErrDesync
	}
	if len(s.buf)-s.off < HeaderSize {
		return Message{}, false, nil
	}

	h := s.buf[s.off : s.off+HeaderSize]
	var hdr Header
	hdr.Length = binary.BigEndian.Uint32(h[0:4])

	if hdr.Length > specialLengthThreshold {
		hdr.Special = true
		raw := s.buf[s.off : s.off+HeaderSize]
		s.off += HeaderSize
		return Message{Header: hdr, Raw: raw}, true, nil
	}

	hdr.Seq = binary.BigEndian.Uint16(h[4:6])
	hdr.EchoSeq = binary.BigEndian.Uint16(h[6:8])
	hdr.Type = MessageType(binary.BigEndian.Uint16(h[8:10]))
	hdr.Checksum = binary.BigEndian.Uint16(h[10:12])

	total := HeaderSize + int(hdr.Length)
	if len(s.buf)-s.off < total {
		return Message{}, false, nil
	}

	raw := s.buf[s.off : s.off+total]
	msg := Message{Header: hdr, Body: raw[HeaderSize:], Raw: raw}

	if s.Checked && !msg.ChecksumValid() {
		s.desync = true
		return Message{}, false, ErrDesync
	}

	s.off += total
	return msg, true, nil
}

// Build assembles a complete on-wire message, computing the checksum.
// Used by tests and by any future server that consumes this package.
func Build(t MessageType, seq, echoSeq uint16, body []byte) []byte {
	out := make([]byte, HeaderSize+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(body)))
	binary.BigEndian.PutUint16(out[4:6], seq)
	binary.BigEndian.PutUint16(out[6:8], echoSeq)
	binary.BigEndian.PutUint16(out[8:10], uint16(t))
	binary.BigEndian.PutUint16(out[10:12], Checksum(uint32(len(body)), seq, echoSeq, uint16(t), body))
	copy(out[HeaderSize:], body)
	return out
}
