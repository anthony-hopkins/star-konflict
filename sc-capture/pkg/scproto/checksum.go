package scproto

// Checksum seed and multiplier for the protocol's MurmurHash2 variant.
const (
	checksumSeed = uint32(0x1337533d)
	murmurM      = uint32(0x5bd1e995)
)

// Checksum computes the 16-bit header checksum.
//
// The trap, and the single likeliest source of a silent reimplementation bug:
// the wire format is big-endian, but the header is fed to the hash in
// LITTLE-endian order. The original C++ parses the header into native (LE)
// integers and hands that struct to murmur2, so the bytes hashed are not the
// bytes transmitted. Both reference implementations agreed on this before they
// were removed from the workspace, and testdata/golden pins it.
//
// The hashed header is 12 bytes: body length, sequence, echo sequence and
// command type as little-endian integers, then two zero bytes where the
// checksum itself sits.
func Checksum(bodyLen uint32, seq, echoSeq, cmdType uint16, body []byte) uint16 {
	var hdr [12]byte
	putUint32LE(hdr[0:], bodyLen)
	putUint16LE(hdr[4:], seq)
	putUint16LE(hdr[6:], echoSeq)
	putUint16LE(hdr[8:], cmdType)
	// hdr[10:12] stay zero — the checksum field is not part of its own input.

	h := uint32(HeaderSize) ^ checksumSeed
	for _, data := range [2][]byte{hdr[:], body} {
		i := 0
		for i+4 <= len(data) {
			k := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
			k *= murmurM
			k ^= k >> 24
			k *= murmurM
			h = h*murmurM ^ k
			i += 4
		}
		// The tail is folded in without the usual final multiply on the
		// upper bytes — this mirrors the original exactly, including its
		// deviation from textbook MurmurHash2.
		switch rem := len(data) - i; {
		case rem >= 3:
			h ^= uint32(data[i+2]) << 16
			fallthrough
		case rem == 2:
			h ^= uint32(data[i+1]) << 8
			fallthrough
		case rem == 1:
			h = (h ^ uint32(data[i])) * murmurM
		}
	}

	h ^= h >> 13
	h *= murmurM
	h ^= h >> 15
	return uint16(h)
}

func putUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
