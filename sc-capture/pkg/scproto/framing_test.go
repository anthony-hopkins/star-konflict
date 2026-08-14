package scproto

import "testing"

// TestSpecialFramesAreTwelveBytes guards the desync trap.
//
// A header whose length field exceeds 0xfffffc is a sentinel, not a length, and
// the frame is complete in twelve bytes. Reading a body for one consumes the
// start of the next frame, after which every subsequent length is garbage and
// the stream never recovers. The bug is silent — capture continues, decoding
// produces confident nonsense — which is why it gets its own test.
func TestSpecialFramesAreTwelveBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"disconnect", append([]byte{0xff, 0xff, 0xff, 0xff}, make([]byte, 8)...)},
		{"keepalive", append([]byte{0xff, 0xff, 0xff, 0xfe}, make([]byte, 8)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			follower := Build(CSCMDAsyncReq, 7, 0, []byte{0xaa, 0xbb})

			s := NewScanner()
			s.Feed(append(append([]byte{}, tc.raw...), follower...))

			first, ok, err := s.Next()
			if err != nil || !ok {
				t.Fatalf("special frame not parsed: ok=%v err=%v", ok, err)
			}
			if !first.Header.Special {
				t.Error("frame not recognised as special")
			}
			if len(first.Raw) != HeaderSize {
				t.Errorf("consumed %d bytes, want %d — this over-read desyncs the stream "+
					"permanently", len(first.Raw), HeaderSize)
			}

			second, ok, err := s.Next()
			if err != nil || !ok {
				t.Fatalf("the frame after a special one was lost: ok=%v err=%v", ok, err)
			}
			if second.Header.Type != CSCMDAsyncReq || second.Header.Seq != 7 {
				t.Errorf("stream desynced after a special frame: got type=%v seq=%d",
					second.Header.Type, second.Header.Seq)
			}
		})
	}
}

// TestScannerHandlesArbitraryChunking checks that framing survives the stream
// arriving in whatever pieces the network chose, which is the normal case: a
// message can span segments and a segment can hold several messages.
func TestScannerHandlesArbitraryChunking(t *testing.T) {
	var stream []byte
	const n = 5
	for i := 0; i < n; i++ {
		stream = append(stream, Build(CSCMDAsyncReq, uint16(i), 0,
			[]byte{byte(i), 0x01, 0x02, 0x03, 0x04})...)
	}

	for _, chunk := range []int{1, 3, 7, 12, 13, 64} {
		s := NewScanner()
		var got int
		for off := 0; off < len(stream); off += chunk {
			end := off + chunk
			if end > len(stream) {
				end = len(stream)
			}
			s.Feed(stream[off:end])
			for {
				msg, ok, err := s.Next()
				if err != nil {
					t.Fatalf("chunk=%d: %v", chunk, err)
				}
				if !ok {
					break
				}
				if msg.Header.Seq != uint16(got) {
					t.Errorf("chunk=%d: message %d has seq %d", chunk, got, msg.Header.Seq)
				}
				got++
			}
		}
		if got != n {
			t.Errorf("chunk=%d: recovered %d of %d messages", chunk, got, n)
		}
	}
}

// TestDesyncStopsRatherThanGuessing covers Principle II's required degradation:
// when framing is lost, decoding must stop and say so, not emit plausible
// nonsense. The bytes are already safe in the journal either way.
func TestDesyncStopsRatherThanGuessing(t *testing.T) {
	good := Build(CSCMDAsyncReq, 1, 0, []byte{0x01, 0x02})
	corrupt := append([]byte{}, good...)
	corrupt[11] ^= 0xff // damage the checksum field

	s := NewScanner()
	s.Feed(corrupt)

	if _, ok, err := s.Next(); err == nil || ok {
		t.Fatal("a corrupt frame was accepted; framing errors must be detected")
	}
	if !s.Desynced() {
		t.Error("scanner did not mark the stream desynced")
	}
	// And it must stay stopped rather than resynchronising on a coincidence.
	if _, ok, _ := s.Next(); ok {
		t.Error("scanner resumed emitting messages after a desync")
	}
}

// TestBufferingIsBounded: a corrupt or hostile length must not make the scanner
// buffer without limit while waiting for bytes that will never arrive.
//
// The bound is the protocol's own rather than one we imposed: any length above
// the sentinel threshold is a twelve-byte special frame, so the largest body
// the framing can even express is MaxBodyLength.
func TestBufferingIsBounded(t *testing.T) {
	// Just above the threshold: must be read as a special frame, consuming
	// exactly twelve bytes and buffering nothing.
	above := []byte{0x00, 0xff, 0xff, 0xff, 0, 1, 0, 0, 0, 13, 0, 0}
	s := NewScanner()
	s.Feed(above)
	msg, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("length above the sentinel threshold not handled: ok=%v err=%v", ok, err)
	}
	if !msg.Header.Special {
		t.Error("length above the sentinel threshold was treated as a body length")
	}
	if s.Buffered() != 0 {
		t.Errorf("%d bytes left buffered", s.Buffered())
	}

	// At the threshold: a legitimate (if enormous) body. The scanner must wait
	// for the rest rather than error or over-read.
	at := []byte{0x00, 0xff, 0xff, 0xfc, 0, 1, 0, 0, 0, 13, 0, 0}
	s2 := NewScanner()
	s2.Feed(at)
	if _, ok, err := s2.Next(); ok || err != nil {
		t.Errorf("a body length at the maximum should wait for more bytes: ok=%v err=%v", ok, err)
	}
	if MaxBodyLength != specialLengthThreshold {
		t.Errorf("MaxBodyLength %d no longer matches the sentinel threshold %d",
			MaxBodyLength, specialLengthThreshold)
	}
}

// TestContainerOpcodeExtraction: the inner opcode is what coverage tracks, so
// pulling it out of the two container types has to be right.
func TestContainerOpcodeExtraction(t *testing.T) {
	async := Build(CSCMDAsyncReq, 1, 0, []byte{0x00, 0x09, 0xde, 0xad})
	s := NewScanner()
	s.Feed(async)
	msg, _, _ := s.Next()
	op, ok := msg.AsyncRequestType()
	if !ok || op != ACPlayerCredentials {
		t.Errorf("async opcode = %v (ok=%v), want AC_PLAYER_CREDENTIALS", op, ok)
	}
	if op.String() != "AC_PLAYER_CREDENTIALS" {
		t.Errorf("opcode 9 named %q", op.String())
	}

	note := Build(SCMDNotification, 2, 0, []byte{0x00, 0x00, 0x11})
	s2 := NewScanner()
	s2.Feed(note)
	msg2, _, _ := s2.Next()
	if nt, ok := msg2.NotificationType(); !ok || nt != 0 {
		t.Errorf("notification opcode = %v (ok=%v), want 0", nt, ok)
	}
}

// TestUniverseIsComplete pins the element counts. If an import ever silently
// drops rows, coverage would under-report what is missing — which is the one
// number the project's whole schedule depends on.
func TestUniverseIsComplete(t *testing.T) {
	counts, err := Counts()
	if err != nil {
		t.Fatalf("loading tables: %v", err)
	}
	for kind, want := range map[Kind]int{
		KindMessageType:  39,
		KindAsyncRequest: 249,
		KindNotification: 116,
	} {
		if counts[kind] != want {
			t.Errorf("%s: %d elements, want %d", kind, counts[kind], want)
		}
	}
	if n, _ := Name(KindMessageType, 11); n != "SCMD_CONNECT_DEDICATED_SERVER" {
		t.Errorf("message type 11 named %q, want SCMD_CONNECT_DEDICATED_SERVER", n)
	}
}
