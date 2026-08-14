package client

import (
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
)

// readPE returns the client executable's architecture and build identity.
//
// The build identity is the field that matters most in this package. A Steam
// build id can be reused and an install directory says nothing, but a build
// identity pins the exact executable that produced a capture — which is what
// somebody in 2031 needs in order to confirm the client they are disassembling
// is the one that spoke the bytes they are reading.
//
// Two sources, in order of strength:
//
//  1. The CodeView PDB signature — a GUID plus an age, stamped by the linker.
//     This is the direct analogue of a GNU build-id: unique per link, and the
//     key Microsoft's own symbol servers file a binary under. Present whenever
//     the linker emitted a debug directory, which is the common case even for
//     shipped release builds.
//  2. The image identity — TimeDateStamp and SizeOfImage. Always present, and
//     the key symbol servers use for the binary itself rather than its symbols.
//     Weaker than a GUID, since a reproducible build pins the timestamp to a
//     constant, but far better than nothing.
//
// Which one was used is recorded alongside the value, because a reader
// comparing two sessions must not mistake one kind of identity for the other.
func readPE(path string) (arch, buildID, buildIDKind string, err error) {
	f, err := pe.Open(path)
	if err != nil {
		return "", "", "", err
	}
	defer f.Close()

	arch = machineName(f.Machine)

	debugRVA, debugSize := debugDirectory(f)
	if debugRVA != 0 && debugSize != 0 {
		if id, err := codeViewID(path, f, debugRVA, debugSize); err == nil && id != "" {
			return arch, id, "codeview", nil
		}
	}

	// Fall back to the image identity. SizeOfImage lives in the optional
	// header, whose type differs between 32- and 64-bit images.
	var sizeOfImage uint32
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		sizeOfImage = oh.SizeOfImage
	case *pe.OptionalHeader64:
		sizeOfImage = oh.SizeOfImage
	default:
		return arch, "", "", nil
	}
	return arch, fmt.Sprintf("%08X%x", f.FileHeader.TimeDateStamp, sizeOfImage), "image", nil
}

func machineName(m uint16) string {
	switch m {
	case pe.IMAGE_FILE_MACHINE_I386:
		return "I386 (32-bit)"
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "AMD64 (64-bit)"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "ARM64 (64-bit)"
	case pe.IMAGE_FILE_MACHINE_ARM, pe.IMAGE_FILE_MACHINE_ARMNT:
		return "ARM (32-bit)"
	default:
		return fmt.Sprintf("machine 0x%04x", m)
	}
}

// debugDirectory returns the RVA and size of the debug data directory.
func debugDirectory(f *pe.File) (rva, size uint32) {
	const entryDebug = 6 // IMAGE_DIRECTORY_ENTRY_DEBUG
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if int(oh.NumberOfRvaAndSizes) <= entryDebug {
			return 0, 0
		}
		d := oh.DataDirectory[entryDebug]
		return d.VirtualAddress, d.Size
	case *pe.OptionalHeader64:
		if int(oh.NumberOfRvaAndSizes) <= entryDebug {
			return 0, 0
		}
		d := oh.DataDirectory[entryDebug]
		return d.VirtualAddress, d.Size
	}
	return 0, 0
}

// debugEntrySize is sizeof(IMAGE_DEBUG_DIRECTORY).
const debugEntrySize = 28

// codeViewTypeID is IMAGE_DEBUG_TYPE_CODEVIEW.
const codeViewTypeID = 2

// codeViewID walks the debug directory for a CodeView record and renders its
// PDB signature in the canonical symbol-server form.
func codeViewID(path string, f *pe.File, rva, size uint32) (string, error) {
	sec := sectionForRVA(f, rva)
	if sec == nil {
		return "", fmt.Errorf("no section contains the debug directory")
	}
	data, err := sec.Data()
	if err != nil {
		return "", err
	}
	off := int(rva - sec.VirtualAddress)
	if off < 0 || off > len(data) {
		return "", fmt.Errorf("debug directory outside its section")
	}
	// Section.Data() returns the raw, on-disk size, which can be shorter than
	// the virtual size the directory was measured against.
	end := off + int(size)
	if end > len(data) {
		end = len(data)
	}

	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()

	for p := off; p+debugEntrySize <= end; p += debugEntrySize {
		e := data[p : p+debugEntrySize]
		if binary.LittleEndian.Uint32(e[12:16]) != codeViewTypeID {
			continue
		}
		sizeOfData := binary.LittleEndian.Uint32(e[16:20])
		pointerToRaw := binary.LittleEndian.Uint32(e[24:28])
		if pointerToRaw == 0 || sizeOfData < 24 {
			continue
		}
		// Cap the read: a corrupt or hostile header must not turn into a
		// multi-gigabyte allocation while identifying a game client.
		if sizeOfData > 1<<16 {
			sizeOfData = 1 << 16
		}
		buf := make([]byte, sizeOfData)
		if _, err := fh.ReadAt(buf, int64(pointerToRaw)); err != nil {
			continue
		}
		if id, ok := parseRSDS(buf); ok {
			return id, nil
		}
	}
	return "", nil
}

func sectionForRVA(f *pe.File, rva uint32) *pe.Section {
	for _, s := range f.Sections {
		if rva >= s.VirtualAddress && rva < s.VirtualAddress+s.VirtualSize {
			return s
		}
	}
	return nil
}

// rsdsSignature is 'RSDS' little-endian: the CodeView PDB 7.0 record.
const rsdsSignature = 0x53445352

// parseRSDS renders a CodeView record as GUID + age, the form a symbol server
// files a PDB under. The trailing PDB path is deliberately not returned: it is
// the build machine's directory layout, which identifies the developer's box
// rather than the binary.
func parseRSDS(b []byte) (string, bool) {
	if len(b) < 24 || binary.LittleEndian.Uint32(b[0:4]) != rsdsSignature {
		return "", false
	}
	// The GUID's first three fields are little-endian integers; the last eight
	// bytes are a byte array. Rendering them any other way produces a string
	// that will not match the symbol server, which is the whole point of it.
	d1 := binary.LittleEndian.Uint32(b[4:8])
	d2 := binary.LittleEndian.Uint16(b[8:10])
	d3 := binary.LittleEndian.Uint16(b[10:12])
	d4 := b[12:20]
	age := binary.LittleEndian.Uint32(b[20:24])

	return fmt.Sprintf("%08X%04X%04X%02X%02X%02X%02X%02X%02X%02X%02X%X",
		d1, d2, d3, d4[0], d4[1], d4[2], d4[3], d4[4], d4[5], d4[6], d4[7], age), true
}
