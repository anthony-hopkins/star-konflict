// Package flow identifies which connection a frame belongs to and which game
// service it represents.
//
// Classification never gates capture. An unclassifiable flow is journaled
// identically to a recognised one — it is simply labelled "unknown", which is a
// statement about our understanding, not about the traffic's worth (FR-005).
package flow

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Service is a logical game service.
type Service string

const (
	ServiceLoadBalancer Service = "loadbalancer"
	ServiceShard        Service = "shard"
	ServiceChat         Service = "chat"
	ServiceWeb          Service = "web"
	ServiceDedicated    Service = "dedicated"
	ServiceUnknown      Service = "unknown"
)

// Evidence records how a service classification was reached. The distinction
// matters: a port is a guess that happens to be usually right, whereas the
// handoff message is the server telling us where it just sent the client.
type Evidence string

const (
	EvidencePort      Evidence = "port"
	EvidenceHandoff   Evidence = "handoff"
	EvidenceHeuristic Evidence = "heuristic"
)

// Direction of travel for a record.
type Direction string

const (
	ClientToServer Direction = "c2s"
	ServerToClient Direction = "s2c"
)

// Master-server ports, per the protocol context in the constitution. These are
// assumptions to verify, not ground truth — hence EvidencePort rather than
// certainty.
var servicePorts = map[uint16]Service{
	3801: ServiceLoadBalancer,
	3802: ServiceShard,
	3815: ServiceChat,
	80:   ServiceWeb,
	443:  ServiceWeb,
}

// Key identifies a bidirectional flow. It is canonicalised so both directions
// map to the same entry.
type Key struct {
	A, B         string
	APort, BPort uint16
	UDP          bool
}

// NewKey canonicalises an endpoint pair.
func NewKey(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, udp bool) (k Key, flipped bool) {
	s, d := srcIP.String(), dstIP.String()
	if s < d || (s == d && srcPort <= dstPort) {
		return Key{A: s, APort: srcPort, B: d, BPort: dstPort, UDP: udp}, false
	}
	return Key{A: d, APort: dstPort, B: s, BPort: srcPort, UDP: udp}, true
}

// Endpoints describes a connection's two ends as seen on the wire.
type Endpoints struct {
	ClientIP   string `json:"client_ip"`
	ClientPort uint16 `json:"client_port"`
	ServerIP   string `json:"server_ip"`
	ServerPort uint16 `json:"server_port"`
}

// Connection is one observed path between the client and an upstream service.
type Connection struct {
	ConnID          string    `json:"conn_id"`
	Transport       string    `json:"transport"`
	Service         Service   `json:"service"`
	ServiceEvidence Evidence  `json:"service_evidence"`
	Rewritten       bool      `json:"rewritten"`
	ReassemblyState string    `json:"reassembly_state"`
	DesyncAtFrame   *uint64   `json:"desync_at_frame"`
	Endpoints       Endpoints `json:"endpoints"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`

	key Key
}

// Table tracks every connection observed in a session.
type Table struct {
	mu     sync.Mutex
	byKey  map[Key]*Connection
	order  []*Key
	nextID int

	// dedicated holds endpoints announced by a handoff message but not yet
	// seen carrying traffic. When a flow to one appears, it is classified from
	// the protocol rather than guessed from a port.
	dedicated map[string]uint64 // "ip:port" -> session id
}

func NewTable() *Table {
	return &Table{byKey: map[Key]*Connection{}, dedicated: map[string]uint64{}}
}

// Observe returns the connection for a packet, creating it on first sight, and
// reports the direction of travel.
func (t *Table) Observe(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, udp bool, at time.Time) (*Connection, Direction) {
	key, flipped := NewKey(srcIP, srcPort, dstIP, dstPort, udp)

	t.mu.Lock()
	defer t.mu.Unlock()

	c, ok := t.byKey[key]
	if !ok {
		t.nextID++
		transport := "tcp"
		if udp {
			transport = "udp"
		}
		c = &Connection{
			ConnID:          fmt.Sprintf("c%03d", t.nextID),
			Transport:       transport,
			ReassemblyState: "ok",
			FirstSeen:       at.UTC(),
			key:             key,
		}
		// Decide which end is the server before classifying, because
		// "which port is the service" and "which side is the client" are the
		// same question.
		c.Endpoints, c.Service, c.ServiceEvidence = t.classifyLocked(srcIP, srcPort, dstIP, dstPort)
		t.byKey[key] = c
		k := key
		t.order = append(t.order, &k)
	}
	c.LastSeen = at.UTC()

	dir := ClientToServer
	server := c.Endpoints.ServerIP
	serverPort := c.Endpoints.ServerPort
	if dstIP.String() != server || dstPort != serverPort {
		dir = ServerToClient
	}
	_ = flipped
	return c, dir
}

// classifyLocked decides the service and which end is the server.
func (t *Table) classifyLocked(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16) (Endpoints, Service, Evidence) {
	// A dedicated-server endpoint announced by a handoff outranks any port
	// heuristic: it is the server telling us where it sent the client.
	if _, ok := t.dedicated[addrKey(dstIP, dstPort)]; ok {
		return Endpoints{ClientIP: srcIP.String(), ClientPort: srcPort,
			ServerIP: dstIP.String(), ServerPort: dstPort}, ServiceDedicated, EvidenceHandoff
	}
	if _, ok := t.dedicated[addrKey(srcIP, srcPort)]; ok {
		return Endpoints{ClientIP: dstIP.String(), ClientPort: dstPort,
			ServerIP: srcIP.String(), ServerPort: srcPort}, ServiceDedicated, EvidenceHandoff
	}

	if svc, ok := servicePorts[dstPort]; ok {
		return Endpoints{ClientIP: srcIP.String(), ClientPort: srcPort,
			ServerIP: dstIP.String(), ServerPort: dstPort}, svc, EvidencePort
	}
	if svc, ok := servicePorts[srcPort]; ok {
		return Endpoints{ClientIP: dstIP.String(), ClientPort: dstPort,
			ServerIP: srcIP.String(), ServerPort: srcPort}, svc, EvidencePort
	}

	// Unknown: assume the lower port is the server, which is right far more
	// often than not, and label the guess honestly.
	if dstPort < srcPort {
		return Endpoints{ClientIP: srcIP.String(), ClientPort: srcPort,
			ServerIP: dstIP.String(), ServerPort: dstPort}, ServiceUnknown, EvidenceHeuristic
	}
	return Endpoints{ClientIP: dstIP.String(), ClientPort: dstPort,
		ServerIP: srcIP.String(), ServerPort: srcPort}, ServiceUnknown, EvidenceHeuristic
}

// AnnounceDedicated records a handoff, so the UDP flow that follows is
// classified from the protocol rather than guessed (FR-011).
func (t *Table) AnnounceDedicated(ip string, port uint16, sessionID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dedicated[fmt.Sprintf("%s:%d", ip, port)] = sessionID

	// A flow to this endpoint may already exist — the client can connect
	// before we finish parsing the message that announced it. Reclassify.
	for _, c := range t.byKey {
		if c.Endpoints.ServerIP == ip && c.Endpoints.ServerPort == port {
			c.Service = ServiceDedicated
			c.ServiceEvidence = EvidenceHandoff
		}
	}
}

// MarkDesync records where framing was lost on a connection. Capture is
// unaffected; only interpretation degrades.
func (t *Table) MarkDesync(c *Connection, frameIndex uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c.ReassemblyState == "desynced" {
		return
	}
	c.ReassemblyState = "desynced"
	f := frameIndex
	c.DesyncAtFrame = &f
}

// Connections returns a snapshot in first-seen order.
func (t *Table) Connections() []Connection {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Connection, 0, len(t.order))
	for _, k := range t.order {
		if c, ok := t.byKey[*k]; ok {
			out = append(out, *c)
		}
	}
	return out
}

// Services returns the distinct services observed.
func (t *Table) Services() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[Service]bool{}
	var out []string
	for _, c := range t.byKey {
		if !seen[c.Service] {
			seen[c.Service] = true
			out = append(out, string(c.Service))
		}
	}
	return out
}

func addrKey(ip net.IP, port uint16) string { return fmt.Sprintf("%s:%d", ip, port) }
