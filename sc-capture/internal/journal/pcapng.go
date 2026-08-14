// Package journal writes the raw evidence: pcapng segments and the hashes over
// them.
//
// It must never import internal/decode, directly or transitively. An
// architecture test enforces this (tests/architecture). The rule is what keeps
// a decoder out of the byte path, which is the whole of Principle II.
package journal

import (
	"bufio"
	"fmt"
	"os"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

// nanosecondResolution is pcapng's if_tsresol = 9. Microseconds are the format
// default and are not good enough: at a 30 Hz tick rate, microsecond precision
// is fine but the header cost of asking for nanoseconds is zero, and questions
// about inter-packet timing deserve the resolution the kernel already provides.
const nanosecondResolution = 9

// InterfaceSpec describes an interface to declare in each segment.
type InterfaceSpec struct {
	Name        string
	Description string // role: game-uplink, loopback-relay-leg
	SnapLength  uint32 // 0 = whole frame
	Filter      string // empty by default; non-empty is a capture-time discard
}

// segment is one pcapng file.
type segment struct {
	path string
	f    *os.File
	bw   *bufio.Writer
	ng   *pcapgo.NgWriter

	frames    uint64
	bytes     uint64
	unflushed uint64
}

// openSegment creates a pcapng file and declares every interface in it.
//
// Interfaces are declared in the same order in every segment, so the interface
// ids referenced by the record index stay valid across a rotation.
func openSegment(path string, ifaces []InterfaceSpec, appName, osName string) (*segment, error) {
	if len(ifaces) == 0 {
		return nil, fmt.Errorf("no interfaces to declare")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create segment: %w", err)
	}
	bw := bufio.NewWriterSize(f, 1<<20)

	ng, err := pcapgo.NewNgWriterInterface(bw, ngIface(ifaces[0]), pcapgo.NgWriterOptions{
		SectionInfo: pcapgo.NgSectionInfo{
			Application: appName,
			OS:          osName,
			Comment: "Star Conflict protocol capture. Raw evidence: unfiltered, " +
				"unmodified, byte-exact. See session.json for metadata.",
		},
	})
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("write section header: %w", err)
	}

	for _, spec := range ifaces[1:] {
		if _, err := ng.AddInterface(ngIface(spec)); err != nil {
			f.Close()
			return nil, fmt.Errorf("declare interface %s: %w", spec.Name, err)
		}
	}

	return &segment{path: path, f: f, bw: bw, ng: ng}, nil
}

func ngIface(s InterfaceSpec) pcapgo.NgInterface {
	return pcapgo.NgInterface{
		Name:                s.Name,
		Description:         s.Description,
		Filter:              s.Filter,
		LinkType:            layers.LinkTypeEthernet,
		SnapLength:          s.SnapLength,
		TimestampResolution: nanosecondResolution,
	}
}

func (s *segment) write(ifaceID int, ci gopacket.CaptureInfo, data []byte) error {
	ci.InterfaceIndex = ifaceID
	if err := s.ng.WritePacket(ci, data); err != nil {
		return err
	}
	s.frames++
	s.bytes += uint64(len(data))
	s.unflushed += uint64(len(data))
	return nil
}

// sync flushes buffered data through to stable storage.
//
// This bounds what an abrupt termination can cost to the flush interval
// (FR-006). fsync per packet would be correct too, and would also mean dropping
// frames at any realistic rate.
func (s *segment) sync() error {
	if err := s.ng.Flush(); err != nil {
		return err
	}
	if err := s.bw.Flush(); err != nil {
		return err
	}
	if err := s.f.Sync(); err != nil {
		return err
	}
	s.unflushed = 0
	return nil
}

func (s *segment) close() error {
	if err := s.ng.Flush(); err != nil {
		return err
	}
	if err := s.bw.Flush(); err != nil {
		return err
	}
	if err := s.f.Sync(); err != nil {
		return err
	}
	return s.f.Close()
}
