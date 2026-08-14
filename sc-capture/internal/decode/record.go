// Package decode turns journaled frames into interpreted records.
//
// It is a CONSUMER of the journal, never a participant in producing it. That
// direction is the whole of Principle II: everything here can panic, desync or
// misread without costing a byte, because the byte was durable before this
// package ever saw it. An architecture test enforces that internal/journal
// cannot import this package.
package decode

import "time"

// Status of an attempted interpretation.
const (
	// StatusDecoded means type and body were both understood.
	StatusDecoded = "decoded"
	// StatusUndecoded means the element is recognised but its body layout is
	// not known. The bytes are safe; the meaning is not known yet. This is a
	// perfectly good outcome — an element observed but not decodable is safe
	// forever, whereas one never observed dies with the servers.
	StatusUndecoded = "undecoded"
	// StatusUnknownElement means an opcode or type absent from the embedded
	// universe. A discovery, not an error.
	StatusUnknownElement = "unknown_element"
	// StatusFailed means the decoder errored or panicked on this record.
	StatusFailed = "failed"
)

// Element kinds, mirroring the protocol tables.
const (
	KindMessageType  = "message_type"
	KindAsyncRequest = "async_request"
	KindNotification = "notification"
)

// FrameRef points at one frame in the raw journal.
//
// References are (segment, index) rather than a session-global frame number, so
// losing one segment does not invalidate references into the others.
type FrameRef struct {
	Segment string `json:"segment"`
	Index   uint64 `json:"index"`
}

// Result is the interpretation of a record. Never a substitute for its bytes.
type Result struct {
	Status         string         `json:"status"`
	MessageType    string         `json:"message_type,omitempty"`
	MessageTypeID  *uint16        `json:"message_type_id,omitempty"`
	Element        string         `json:"element,omitempty"`
	ElementID      *uint16        `json:"element_id,omitempty"`
	ElementKind    string         `json:"element_kind,omitempty"`
	Fields         map[string]any `json:"fields,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Novel          bool           `json:"novel,omitempty"`
	DecoderVersion string         `json:"decoder_version,omitempty"`
}

// Record is one protocol-level unit: a datagram, or one framed message on a
// reassembled stream. Serialised as a line of index.jsonl.
//
// Entirely derived data. A missing, truncated or corrupt index never
// invalidates a session, and `sccap index --rebuild` regenerates it from the
// pcapng segments alone.
type Record struct {
	Seq       uint64     `json:"seq"`
	ConnID    string     `json:"conn_id"`
	Dir       string     `json:"dir"`
	TWall     time.Time  `json:"t_wall"`
	TMono     int64      `json:"t_mono"`
	Frames    []FrameRef `json:"frames"`
	ByteLen   int        `json:"byte_len"`
	Decode    *Result    `json:"decode"`
	Annotated *int       `json:"annotation_seq,omitempty"`
}
