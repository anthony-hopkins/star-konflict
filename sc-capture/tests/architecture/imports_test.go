// Package architecture enforces the two import rules that make this design's
// guarantees structural rather than aspirational.
//
// Both rules exist because a reviewer cannot reliably catch their violation by
// reading a diff: the damage arrives as an innocuous-looking import three
// packages away. If either test fails, a constitutional principle has been
// broken, and no amount of careful coding elsewhere restores it.
package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/sc-re/sc-capture"

// repoRoot walks up from this test file to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not locate go.mod above the test directory")
	return ""
}

// packageImports maps each in-module package path to the packages it imports.
func packageImports(t *testing.T) map[string][]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string][]string{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "out", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not our business to fail on unparseable files here
		}

		rel, rerr := filepath.Rel(root, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		pkg := modulePath
		if rel != "." {
			pkg = modulePath + "/" + filepath.ToSlash(rel)
		}

		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			out[pkg] = append(out[pkg], p)
		}
		if _, ok := out[pkg]; !ok {
			out[pkg] = nil
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

// reaches reports whether from can reach any package under prefix, following
// only in-module edges. Returns the path taken, for a useful failure message.
func reaches(graph map[string][]string, from, prefix string) []string {
	type node struct {
		pkg  string
		path []string
	}
	seen := map[string]bool{from: true}
	queue := []node{{pkg: from, path: []string{from}}}

	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, imp := range graph[n.pkg] {
			if !strings.HasPrefix(imp, modulePath) {
				continue // stdlib or third party
			}
			if strings.HasPrefix(imp, prefix) {
				return append(append([]string{}, n.path...), imp)
			}
			if seen[imp] {
				continue
			}
			seen[imp] = true
			queue = append(queue, node{pkg: imp, path: append(append([]string{}, n.path...), imp)})
		}
	}
	return nil
}

// TestJournalDoesNotImportDecode enforces Principle II: raw bytes are evidence,
// and no decoder may occupy a position where its failure causes byte loss.
//
// The whole architecture rests on decode being downstream of an already-durable
// journal. The moment the journal can call into decode, a panic or a desync in
// a decoder becomes capable of costing bytes, and the guarantee is gone — not
// weakened, gone, and unrecoverably so for anything captured in the meantime.
func TestJournalDoesNotImportDecode(t *testing.T) {
	graph := packageImports(t)

	for _, writer := range []string{
		modulePath + "/internal/journal",
		modulePath + "/internal/capture",
	} {
		if _, ok := graph[writer]; !ok {
			continue // package not written yet
		}
		if path := reaches(graph, writer, modulePath+"/internal/decode"); path != nil {
			t.Errorf("PRINCIPLE II VIOLATION: the write path reaches a decoder.\n"+
				"  %s\n"+
				"A decoder that panics or desyncs can now cost bytes. Decode must consume\n"+
				"the journal, never participate in producing it.", strings.Join(path, "\n    -> "))
		}
	}
}

// TestProtoIsSelfContained enforces Principle VI: one protocol implementation,
// in a package with no capture, storage or transport dependencies, so a future
// server reimplementation can consume it unchanged.
//
// The dependency rule is what keeps that promise cheap to honour. It is also
// what stops a second parser appearing behind a convenience wrapper: if
// scproto cannot import the rest of the tree, nobody can quietly grow a
// parallel decoding path inside it.
func TestProtoIsSelfContained(t *testing.T) {
	graph := packageImports(t)
	proto := modulePath + "/pkg/scproto"

	for pkg, imports := range graph {
		if !strings.HasPrefix(pkg, proto) {
			continue
		}
		for _, imp := range imports {
			switch {
			case strings.HasPrefix(imp, proto):
				// scproto may import its own subpackages.
			case strings.HasPrefix(imp, modulePath):
				t.Errorf("PRINCIPLE VI VIOLATION: %s imports %s.\n"+
					"pkg/scproto must depend on nothing else in this module, so that a\n"+
					"server reimplementation can consume it unchanged.", pkg, imp)
			case strings.Contains(strings.Split(imp, "/")[0], "."):
				// A dotted first segment means a hosted module, not stdlib.
				t.Errorf("PRINCIPLE VI VIOLATION: %s imports third-party package %s.\n"+
					"pkg/scproto must use only the standard library.", pkg, imp)
			}
		}
	}
}
