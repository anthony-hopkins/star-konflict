//go:build npcap

package capture

import (
	"fmt"
	"net"
	"strings"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcap"
)

// The Npcap backend. Selected with `-tags npcap`, and the only way this tool
// records anything.
//
// This is the one part of the project that needs cgo, and it is gated behind a
// build tag for that reason: without the tag the binary still builds statically
// and every offline command — verify, decode, index, coverage — works on an
// archived session. Only live capture needs the tag.
//
// Npcap is a separate install and its licence does not permit redistribution,
// so it cannot be bundled. `sccap doctor` reports its absence with the download
// location rather than failing mysteriously at capture time.

type pcapSource struct {
	handle *pcap.Handle
}

func available() (bool, string) { return true, "" }

// openSource opens a live capture handle.
func openSource(iface string, snaplen int) (source, error) {
	size := int32(frameSize)
	if snaplen > 0 {
		size = int32(snaplen)
	}

	device, err := resolveDevice(iface)
	if err != nil {
		return nil, err
	}

	// Immediate mode, not a timeout: buffering frames to fill a batch would
	// delay the drop counter that tells a contributor their capture is going
	// wrong, and the traffic rates here are far too low to need batching.
	inactive, err := pcap.NewInactiveHandle(device)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", iface, err)
	}
	defer inactive.CleanUp()

	if err := inactive.SetSnapLen(int(size)); err != nil {
		return nil, fmt.Errorf("snaplen: %w", err)
	}
	if err := inactive.SetPromisc(true); err != nil {
		return nil, fmt.Errorf("promiscuous mode: %w", err)
	}
	if err := inactive.SetTimeout(pollWait); err != nil {
		return nil, fmt.Errorf("timeout: %w", err)
	}
	if err := inactive.SetImmediateMode(true); err != nil {
		return nil, fmt.Errorf("immediate mode: %w", err)
	}
	if err := inactive.SetBufferSize(bufferBytes); err != nil {
		return nil, fmt.Errorf("buffer size: %w", err)
	}

	h, err := inactive.Activate()
	if err != nil {
		return nil, fmt.Errorf("activate %s: %w (is Npcap installed, and are you running "+
			"as Administrator?)", iface, err)
	}
	// No BPF filter is ever set. Capture-time filtering is a permanent,
	// irreversible discard decision (Principle I).
	return &pcapSource{handle: h}, nil
}

func (s *pcapSource) read() ([]byte, gopacket.CaptureInfo, error) {
	data, ci, err := s.handle.ZeroCopyReadPacketData()
	if err == pcap.NextErrorTimeoutExpired {
		// A quiet link, not a failure. Callers treat a read error as "nothing
		// arrived this tick" and loop.
		return nil, ci, err
	}
	return data, ci, err
}

func (s *pcapSource) stats() (packets, drops uint64, err error) {
	st, err := s.handle.Stats()
	if err != nil {
		return 0, 0, err
	}
	// PacketsIfDropped counts frames the interface itself discarded, which is
	// as much a hole in the archive as one the driver dropped.
	return uint64(st.PacketsReceived),
		uint64(st.PacketsDropped) + uint64(st.PacketsIfDropped), nil
}

func (s *pcapSource) close() {
	if s.handle != nil {
		s.handle.Close()
	}
}

// resolveDevice maps an adapter name to the device name Npcap knows it by.
//
// These are two different namespaces and neither is derivable from the other.
// Windows shows a contributor "Ethernet" and "Wi-Fi" — in Settings, in
// ipconfig, and in `sccap doctor`'s own interface list, which is built from the
// standard library. Npcap shows \Device\NPF_{GUID}. Accepting only the latter
// would mean every name this tool prints is a name it then refuses, so the
// pre-flight check that exists to stop contributors capturing the wrong wire
// would itself fail on every wire.
//
// Matching is by IP address, not by description. Descriptions are vendor
// marketing text — "Intel(R) Ethernet Connection I219-V" — which two adapters
// in the same machine routinely share, and picking the wrong one produces
// exactly the silent, total failure the interface check exists to prevent.
func resolveDevice(name string) (string, error) {
	if strings.HasPrefix(name, `\Device\`) {
		return name, nil
	}

	devs, err := pcap.FindAllDevs()
	if err != nil {
		return "", fmt.Errorf("enumerating Npcap devices: %w (is Npcap installed?)", err)
	}
	for _, d := range devs {
		if strings.EqualFold(d.Name, name) {
			return d.Name, nil
		}
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("no network interface named %q: %w\n%s", name, err, deviceList(devs))
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("reading addresses of %s: %w", name, err)
	}

	want := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			want[ipnet.IP.String()] = true
		}
	}
	if len(want) == 0 {
		return "", fmt.Errorf("%s has no IP address, so it cannot be matched to an Npcap "+
			"device. Name the device directly.\n%s", name, deviceList(devs))
	}

	for _, d := range devs {
		for _, a := range d.Addresses {
			if a.IP != nil && want[a.IP.String()] {
				return d.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no Npcap device carries an address of %s. Npcap may have been "+
		"installed after this adapter appeared; reinstalling it re-enumerates.\n%s",
		name, deviceList(devs))
}

// deviceList renders what Npcap can see, so a failure names the alternatives
// instead of leaving a contributor guessing.
func deviceList(devs []pcap.Interface) string {
	if len(devs) == 0 {
		return "Npcap reports no capture devices at all."
	}
	var b strings.Builder
	b.WriteString("Npcap devices:")
	for _, d := range devs {
		b.WriteString("\n  " + d.Name)
		if d.Description != "" {
			b.WriteString("  (" + d.Description + ")")
		}
		for _, a := range d.Addresses {
			if a.IP != nil {
				b.WriteString("  " + a.IP.String())
			}
		}
	}
	return b.String()
}
