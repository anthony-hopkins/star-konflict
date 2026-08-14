package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sc-re/sc-capture/internal/decode"
	"github.com/sc-re/sc-capture/internal/exitcode"
	"github.com/sc-re/sc-capture/internal/index"
	"github.com/sc-re/sc-capture/internal/session"
)

// runDecode reads an archived session.
//
// It works with every game service unreachable, because it reads the journal
// rather than the network (FR-029). The filters below are READ-time filtering
// against a complete archive, which the constitution permits explicitly — as
// distinct from capture-time filtering, which it forbids outright.
func runDecode(args []string) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	conn := fs.String("conn", "", "only this connection id")
	msgType := fs.String("type", "", "only this message type or element name")
	status := fs.String("status", "", "decoded | undecoded | unknown_element | failed")
	asJSON := fs.Bool("json", false, "emit machine-readable output on stdout")
	limit := fs.Int("limit", 0, "stop after this many records (0 = all)")
	dir, perr := parsePositional(fs, args)
	if perr != nil || dir == "" {
		errf("usage: sccap decode <bundle-dir> [--conn id] [--type name] [--status s] [--json]")
		return exitcode.Usage
	}

	meta, err := session.LoadMetadata(dir)
	if err != nil {
		if isSchemaError(err) {
			errf("%v", err)
			return exitcode.SchemaUnreadable
		}
		errf("%v", err)
		return exitcode.Usage
	}

	// Prefer the existing index; fall back to decoding from the journal, so
	// this works on a bundle whose derived data was never written or was lost.
	records, err := readIndex(dir)
	if err != nil || len(records) == 0 {
		fmt.Fprintln(os.Stderr, "No usable record index; decoding from the raw journal.")
		_, _, rerr := decode.Replay(dir, meta.UTCStart, func(r decode.Record) {
			records = append(records, r)
		})
		if rerr != nil {
			errf("replay: %v", rerr)
			return exitcode.Usage
		}
	}

	enc := json.NewEncoder(os.Stdout)
	shown := 0
	for _, r := range records {
		if !matches(r, *conn, *msgType, *status) {
			continue
		}
		if *asJSON {
			if err := enc.Encode(r); err != nil {
				errf("%v", err)
				return exitcode.Usage
			}
		} else {
			printRecord(r)
		}
		shown++
		if *limit > 0 && shown >= *limit {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "\n%d of %d record(s).\n", shown, len(records))
	if shown == 0 && len(records) > 0 {
		fmt.Fprintln(os.Stderr, "Nothing matched. Note that filtering here is read-time only; "+
			"the journal always holds everything.")
	}
	return exitcode.OK
}

func printRecord(r decode.Record) {
	d := r.Decode
	label := "(not attempted)"
	detail := ""
	if d != nil {
		label = d.MessageType
		if d.Element != "" {
			label += " / " + d.Element
		}
		switch d.Status {
		case decode.StatusDecoded:
			if len(d.Fields) > 0 {
				b, _ := json.Marshal(d.Fields)
				detail = string(b)
			}
		case decode.StatusUnknownElement:
			// A discovery, not an error — say so where a reader will see it.
			detail = "NOVEL: " + d.Reason
		default:
			// Undecodable records are shown as what they are, never omitted
			// and never dressed up as a partial interpretation (FR-017).
			detail = d.Reason
		}
	}

	fmt.Printf("#%-6d %-4s %-6s %-10s %-44s %s\n",
		r.Seq, r.ConnID, r.Dir, statusOf(d), label, detail)
	if d != nil && d.Status != decode.StatusDecoded && len(r.Frames) > 0 {
		// Point at the evidence, so an undecoded record is still actionable.
		fmt.Printf("        %d bytes; raw at %s frame %d\n",
			r.ByteLen, r.Frames[0].Segment, r.Frames[0].Index)
	}
}

func statusOf(d *decode.Result) string {
	if d == nil {
		return "-"
	}
	return d.Status
}

func matches(r decode.Record, conn, typ, status string) bool {
	if conn != "" && r.ConnID != conn {
		return false
	}
	if status != "" && (r.Decode == nil || r.Decode.Status != status) {
		return false
	}
	if typ != "" {
		if r.Decode == nil {
			return false
		}
		if !strings.EqualFold(r.Decode.MessageType, typ) &&
			!strings.EqualFold(r.Decode.Element, typ) {
			return false
		}
	}
	return true
}

// readIndex loads index.jsonl, tolerating a truncated final line — which is the
// expected shape after an abrupt kill and is not a reason to reject the rest.
func readIndex(dir string) ([]decode.Record, error) {
	f, err := os.Open(filepath.Join(dir, index.File))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []decode.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r decode.Record
		if err := json.Unmarshal(line, &r); err != nil {
			break // torn tail; everything before it stands
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return out, nil
	}
	return out, nil
}
