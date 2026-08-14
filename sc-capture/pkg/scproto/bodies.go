package scproto

import "fmt"

// Only bodies that have actually been observed on the wire are decoded here.
//
// The constitution forbids writing a decoder for a message shape that has not
// been seen: speculative decoding is how a wrong assumption enters an archive
// that outlives the people who could have corrected it. The two below are
// decoded because the capture path genuinely needs them — they are what tell us
// which connection is which service.

// DedicatedServer is the payload of SCMD_CONNECT_DEDICATED_SERVER (type 11).
//
// This is the handoff to the match server: the only pointer to the in-match UDP
// protocol, which no tool in this project has ever recorded. SessionID is the
// field that may or may not be bound to the address the master server
// registered — the open question behind whether a relayed match is possible at
// all.
type DedicatedServer struct {
	Addr      string
	Port      uint16
	SessionID uint64
	ZoneID    int32
	Flag      bool
}

// ParseDedicatedServer decodes the handoff body.
//
// Layout: cstring addr, u16 port, u64 session_id, i32 zone_id, 1 bit flag.
func ParseDedicatedServer(body []byte) (*DedicatedServer, error) {
	r := NewBitReader(body)

	addr, err := r.CString()
	if err != nil {
		return nil, fmt.Errorf("addr: %w", err)
	}
	port, err := r.Uint16()
	if err != nil {
		return nil, fmt.Errorf("port: %w", err)
	}
	sessionID, err := r.Uint64()
	if err != nil {
		return nil, fmt.Errorf("session_id: %w", err)
	}
	zoneID, err := r.Int32()
	if err != nil {
		return nil, fmt.Errorf("zone_id: %w", err)
	}
	// The trailing bit is present but its purpose is not pinned down. It is
	// read so the layout is consumed correctly, and its absence is tolerated
	// rather than treated as a parse failure.
	flag, _ := r.Bool()

	if addr == "" {
		// The client itself logs a warning for this case, so it happens on the
		// real service. Surface it rather than pretending the handoff was fine.
		return &DedicatedServer{Port: port, SessionID: sessionID, ZoneID: zoneID, Flag: flag},
			fmt.Errorf("handoff carried an empty address")
	}
	return &DedicatedServer{
		Addr: addr, Port: port, SessionID: sessionID, ZoneID: zoneID, Flag: flag,
	}, nil
}

// Endpoint is an address/port pair handed out by the load balancer.
type Endpoint struct {
	IP   string
	Port uint16
}

// AssignedShard is the payload of SCMD_ASSIGNED_SHARD (type 0): where the
// client should connect next.
type AssignedShard struct {
	Shards []Endpoint
	Chat   *Endpoint
}

// ParseAssignedShard decodes the shard assignment body.
//
// Layout: bool, u8 count, count × (cstring ip, u16 port), bool, cstring
// chat_ip, u16 chat_port. All bit-packed.
func ParseAssignedShard(body []byte) (*AssignedShard, error) {
	r := NewBitReader(body)

	if _, err := r.Bool(); err != nil {
		return nil, fmt.Errorf("leading flag: %w", err)
	}
	count, err := r.Uint8()
	if err != nil {
		return nil, fmt.Errorf("shard count: %w", err)
	}

	out := &AssignedShard{}
	for i := 0; i < int(count); i++ {
		ip, err := r.CString()
		if err != nil {
			return out, fmt.Errorf("shard %d address: %w", i, err)
		}
		port, err := r.Uint16()
		if err != nil {
			return out, fmt.Errorf("shard %d port: %w", i, err)
		}
		out.Shards = append(out.Shards, Endpoint{IP: ip, Port: port})
	}

	hasChat, err := r.Bool()
	if err != nil {
		// Everything before this point is still good; return what we have.
		return out, nil
	}
	if !hasChat {
		return out, nil
	}
	ip, err := r.CString()
	if err != nil {
		return out, fmt.Errorf("chat address: %w", err)
	}
	port, err := r.Uint16()
	if err != nil {
		return out, fmt.Errorf("chat port: %w", err)
	}
	out.Chat = &Endpoint{IP: ip, Port: port}
	return out, nil
}
