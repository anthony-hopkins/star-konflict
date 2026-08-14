package scproto

import "errors"

// ErrBitReaderEOF reports a read past the end of the buffer.
var ErrBitReaderEOF = errors.New("bit reader: past end of buffer")

// BitReader reads MSB-first bit fields.
//
// Several message bodies are bit-packed rather than byte-aligned — the shard
// assignment and the dedicated-server handoff among them — so a byte reader
// cannot parse them at all. Bits are consumed most-significant-first within
// each byte, which is the order the original writer emits them.
type BitReader struct {
	buf []byte
	bit int // absolute bit offset from the start of buf
}

// NewBitReader returns a reader over b.
func NewBitReader(b []byte) *BitReader { return &BitReader{buf: b} }

// BitOffset returns how many bits have been consumed.
func (r *BitReader) BitOffset() int { return r.bit }

// Remaining returns how many bits are left.
func (r *BitReader) Remaining() int { return len(r.buf)*8 - r.bit }

func (r *BitReader) readBit() (uint64, error) {
	idx, off := r.bit>>3, r.bit&7
	if idx >= len(r.buf) {
		return 0, ErrBitReaderEOF
	}
	v := (r.buf[idx] >> (7 - off)) & 1
	r.bit++
	return uint64(v), nil
}

// Uint reads n bits (n <= 64) as an unsigned value.
func (r *BitReader) Uint(n int) (uint64, error) {
	if n < 0 || n > 64 {
		return 0, errors.New("bit reader: bad width")
	}
	var v uint64
	for i := 0; i < n; i++ {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | b
	}
	return v, nil
}

// Bool reads a single bit.
func (r *BitReader) Bool() (bool, error) {
	v, err := r.Uint(1)
	return v == 1, err
}

func (r *BitReader) Uint8() (uint8, error) {
	v, err := r.Uint(8)
	return uint8(v), err
}

func (r *BitReader) Uint16() (uint16, error) {
	v, err := r.Uint(16)
	return uint16(v), err
}

func (r *BitReader) Uint32() (uint32, error) {
	v, err := r.Uint(32)
	return uint32(v), err
}

func (r *BitReader) Uint64() (uint64, error) { return r.Uint(64) }

// Int32 reads a 32-bit two's-complement value.
func (r *BitReader) Int32() (int32, error) {
	v, err := r.Uint(32)
	return int32(uint32(v)), err
}

// MaxStringLength bounds a NUL-terminated string so a desynced or corrupt body
// cannot make the reader spin to the end of a large buffer.
const MaxStringLength = 4096

// CString reads bytes up to a NUL terminator.
func (r *BitReader) CString() (string, error) {
	out := make([]byte, 0, 32)
	for len(out) < MaxStringLength {
		c, err := r.Uint8()
		if err != nil {
			return "", err
		}
		if c == 0 {
			return string(out), nil
		}
		out = append(out, c)
	}
	return "", errors.New("bit reader: unterminated string")
}
