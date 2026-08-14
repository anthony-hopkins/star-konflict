package doctor

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"
)

// Master-server ports. Presence of traffic on these is strong evidence that an
// interface is the one carrying the game.
var gamePorts = map[uint16]string{
	3801: "loadbalancer",
	3802: "shard",
	3815: "chat",
}

// InterfaceTraffic is what --watch observed on one interface.
type InterfaceTraffic struct {
	Name        string         `json:"name"`
	Packets     uint64         `json:"packets"`
	GamePackets uint64         `json:"game_packets"`
	Services    map[string]int `json:"services"`
	UDPPeers    map[string]int `json:"udp_peers"`
	Err         string         `json:"error,omitempty"`
}

// Watch samples every live interface for d and reports which of them actually
// carry traffic to game endpoints.
//
// This is the check that matters most. Capturing on the wrong interface
// produces a session that passes every other test in the suite — zero drops, a
// valid bundle, clean verification — and contains none of the game's traffic.
// It is the only failure mode here that is both silent and total, and it is
// what the network-namespace scripts used to prevent by construction.
func Watch(d time.Duration, only string) ([]InterfaceTraffic, error) {
	ifs, err := Interfaces()
	if err != nil {
		return nil, err
	}

	var candidates []InterfaceInfo
	for _, i := range ifs {
		if !i.Up {
			continue
		}
		if only != "" && i.Name != only {
			continue
		}
		if only == "" && i.Loopback {
			// Loopback matters only when relaying; for "which wire is the
			// game on" it is noise.
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no live interfaces to watch")
	}

	results := make([]InterfaceTraffic, len(candidates))
	var wg sync.WaitGroup
	for idx, iface := range candidates {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = sample(name, d)
		}(idx, iface.Name)
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].GamePackets != results[j].GamePackets {
			return results[i].GamePackets > results[j].GamePackets
		}
		return results[i].Packets > results[j].Packets
	})
	return results, nil
}

func sample(iface string, d time.Duration) InterfaceTraffic {
	out := InterfaceTraffic{
		Name:     iface,
		Services: map[string]int{},
		UDPPeers: map[string]int{},
	}

	ni, err := net.InterfaceByName(iface)
	if err != nil {
		out.Err = err.Error()
		return out
	}

	// ETH_P_ALL in network byte order.
	const ethPAll = 0x0003
	proto := int(uint16(ethPAll<<8) | uint16(ethPAll>>8))
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, proto)
	if err != nil {
		out.Err = "socket: " + err.Error() + " (missing CAP_NET_RAW?)"
		return out
	}
	defer syscall.Close(fd)

	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: uint16(proto),
		Ifindex:  ni.Index,
	}); err != nil {
		out.Err = "bind: " + err.Error()
		return out
	}

	// Read timeout so we can stop at the deadline even on a silent link.
	tv := syscall.Timeval{Sec: 0, Usec: 200_000}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)

	buf := make([]byte, 65536)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
				continue
			}
			out.Err = "recv: " + err.Error()
			return out
		}
		if n <= 0 {
			continue
		}
		out.Packets++
		classify(buf[:n], &out)
	}
	return out
}

// classify does just enough header walking to find transport ports. It is
// deliberately minimal — this is a diagnostic, not the capture path, and it
// never needs to be exhaustive to answer "is the game on this wire".
func classify(b []byte, out *InterfaceTraffic) {
	if len(b) < 14 {
		return
	}
	et := binary.BigEndian.Uint16(b[12:14])
	off := 14
	// Walk one or two VLAN tags.
	for (et == 0x8100 || et == 0x88a8) && len(b) >= off+4 {
		et = binary.BigEndian.Uint16(b[off+2 : off+4])
		off += 4
	}
	if et != 0x0800 || len(b) < off+20 {
		return // IPv4 only; sufficient for this diagnosis
	}
	ip := b[off:]
	ihl := int(ip[0]&0x0f) * 4
	if ihl < 20 || len(ip) < ihl+4 {
		return
	}
	proto := ip[9]
	if proto != 6 && proto != 17 {
		return
	}
	src := binary.BigEndian.Uint16(ip[ihl : ihl+2])
	dst := binary.BigEndian.Uint16(ip[ihl+2 : ihl+4])

	for _, p := range []uint16{src, dst} {
		if name, ok := gamePorts[p]; ok {
			out.GamePackets++
			out.Services[name]++
			return
		}
	}

	// UDP to a remote peer is the shape in-match traffic takes. We cannot
	// identify it as the dedicated server without the handoff message, but a
	// busy remote UDP flow is worth surfacing to a contributor deciding
	// which interface to capture.
	if proto == 17 && len(ip) >= 20 {
		dstIP := net.IP(ip[16:20])
		if !dstIP.IsLoopback() && !dstIP.IsMulticast() && !dstIP.IsLinkLocalUnicast() {
			out.UDPPeers[fmt.Sprintf("%s:%d", dstIP, dst)]++
		}
	}
}
