package scproto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// TestGoldenFraming is the parity check for Principle VI.
//
// The vectors in testdata/golden/framing.json were generated once, by hand,
// from the archived reference implementation (docs/protocol/source/protocol.py,
// public domain). That file is the only independent thing this project can
// check its framing against now that the sibling repositories are gone, which
// is precisely why it is kept.
//
// If this test fails, our idea of the wire format has diverged from the one
// that was actually observed on the real service. Every decode in every archive
// this build produces would be wrong in the same way, and silently.
type goldenFile struct {
	Cases []struct {
		Name      string `json:"name"`
		BodyLen   uint32 `json:"body_len"`
		Seq       uint16 `json:"seq"`
		EchoSeq   uint16 `json:"echo_seq"`
		CmdType   uint16 `json:"cmd_type"`
		BodyHex   string `json:"body_hex"`
		Checksum  uint16 `json:"checksum"`
		PacketHex string `json:"packet_hex"`
	} `json:"cases"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	b, err := os.ReadFile("../../testdata/golden/framing.json")
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	if len(g.Cases) == 0 {
		t.Fatal("golden file contains no cases")
	}
	return g
}

func TestGoldenChecksum(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.Cases {
		body, err := hex.DecodeString(c.BodyHex)
		if err != nil {
			t.Fatalf("%s: bad body hex: %v", c.Name, err)
		}
		got := Checksum(c.BodyLen, c.Seq, c.EchoSeq, c.CmdType, body)
		if got != c.Checksum {
			t.Errorf("%s: checksum = %#04x, reference says %#04x\n"+
				"Our framing has diverged from the observed protocol. The most likely cause "+
				"is byte order: the header is big-endian on the wire but is fed to the hash "+
				"LITTLE-endian.", c.Name, got, c.Checksum)
		}
	}
}

func TestGoldenBuild(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.Cases {
		body, _ := hex.DecodeString(c.BodyHex)
		want, err := hex.DecodeString(c.PacketHex)
		if err != nil {
			t.Fatalf("%s: bad packet hex: %v", c.Name, err)
		}
		got := Build(MessageType(c.CmdType), c.Seq, c.EchoSeq, body)
		if !bytes.Equal(got, want) {
			t.Errorf("%s: built packet differs from reference\n got: %s\nwant: %s",
				c.Name, hex.EncodeToString(got), hex.EncodeToString(want))
		}
	}
}

// TestGoldenRoundTrip runs every reference packet back through the scanner,
// which is the direction that matters for capture: we parse far more than we
// build.
func TestGoldenRoundTrip(t *testing.T) {
	g := loadGolden(t)
	for _, c := range g.Cases {
		raw, _ := hex.DecodeString(c.PacketHex)
		s := NewScanner()
		s.Feed(raw)
		msg, ok, err := s.Next()
		if err != nil {
			t.Errorf("%s: scanner rejected a reference packet: %v", c.Name, err)
			continue
		}
		if !ok {
			t.Errorf("%s: scanner wanted more bytes for a complete packet", c.Name)
			continue
		}
		if uint16(msg.Header.Type) != c.CmdType {
			t.Errorf("%s: type = %d, want %d", c.Name, msg.Header.Type, c.CmdType)
		}
		if msg.Header.Seq != c.Seq || msg.Header.EchoSeq != c.EchoSeq {
			t.Errorf("%s: seq/echo = %d/%d, want %d/%d",
				c.Name, msg.Header.Seq, msg.Header.EchoSeq, c.Seq, c.EchoSeq)
		}
		if !msg.ChecksumValid() {
			t.Errorf("%s: checksum rejected on a reference packet", c.Name)
		}
		if s.Buffered() != 0 {
			t.Errorf("%s: %d bytes left over after one packet", c.Name, s.Buffered())
		}
	}
}
