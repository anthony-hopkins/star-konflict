package client

import (
	"encoding/binary"
	"testing"
)

// rsdsRecord builds a CodeView PDB 7.0 record the way a linker writes one.
func rsdsRecord(d1 uint32, d2, d3 uint16, d4 [8]byte, age uint32, pdb string) []byte {
	b := make([]byte, 24, 24+len(pdb)+1)
	binary.LittleEndian.PutUint32(b[0:4], rsdsSignature)
	binary.LittleEndian.PutUint32(b[4:8], d1)
	binary.LittleEndian.PutUint16(b[8:10], d2)
	binary.LittleEndian.PutUint16(b[10:12], d3)
	copy(b[12:20], d4[:])
	binary.LittleEndian.PutUint32(b[20:24], age)
	b = append(b, pdb...)
	return append(b, 0)
}

// TestParseRSDSMatchesTheSymbolServerForm pins the exact rendering.
//
// The GUID's first three fields are little-endian integers and its last eight
// bytes are a plain array. Rendering all sixteen bytes the same way is the
// classic mistake, and it produces a string that looks entirely plausible and
// matches nothing — so a contributor in 2031 holding this build id could not
// use it to find the executable it names.
func TestParseRSDSMatchesTheSymbolServerForm(t *testing.T) {
	rec := rsdsRecord(
		0x11223344, 0x5566, 0x7788,
		[8]byte{0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00},
		7, `C:\build\StarConflict.pdb`)

	got, ok := parseRSDS(rec)
	if !ok {
		t.Fatal("a well-formed RSDS record was rejected")
	}
	const want = "112233445566778899AABBCCDDEEFF007"
	if got != want {
		t.Errorf("parseRSDS = %q, want %q", got, want)
	}
}

// TestParseRSDSRejectsWhatItShould: a debug directory can carry record types
// this does not understand, and mis-reading one as a build identity would
// stamp a session with a number that means nothing.
func TestParseRSDSRejectsWhatItShould(t *testing.T) {
	for name, rec := range map[string][]byte{
		"empty":            {},
		"short":            make([]byte, 12),
		"wrong signature":  append([]byte("NB10"), make([]byte, 32)...),
		"truncated record": rsdsRecord(1, 2, 3, [8]byte{}, 1, "x")[:20],
	} {
		if _, ok := parseRSDS(rec); ok {
			t.Errorf("%s: accepted a record it should have rejected", name)
		}
	}
}

// TestPDBPathIsNotRecorded: the record carries the build machine's directory
// layout after the age field. That names a developer's box, not the binary,
// and it must not reach a bundle that gets shared.
func TestPDBPathIsNotRecorded(t *testing.T) {
	const secret = `C:\Users\somedev\jenkins\workspace\StarConflict.pdb`
	rec := rsdsRecord(1, 2, 3, [8]byte{4}, 5, secret)

	got, ok := parseRSDS(rec)
	if !ok {
		t.Fatal("record rejected")
	}
	if contains(got, "somedev") || contains(got, "jenkins") || contains(got, ".pdb") {
		t.Errorf("the PDB path leaked into the build id: %q", got)
	}
}

func TestMachineName(t *testing.T) {
	for machine, want := range map[uint16]string{
		0x014c: "I386 (32-bit)",
		0x8664: "AMD64 (64-bit)",
		0xAA64: "ARM64 (64-bit)",
		0x1234: "machine 0x1234",
	} {
		if got := machineName(machine); got != want {
			t.Errorf("machineName(0x%04x) = %q, want %q", machine, got, want)
		}
	}
}
