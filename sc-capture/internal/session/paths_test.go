package session

import (
	"os"
	"path/filepath"
	"testing"
)

// A capture started from any subdirectory of the repository must land in the
// same packet-caps/ at the repository root — two sessions started from
// different directories going to different places is how bundles get lost.
func TestDefaultCaptureDirFindsRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	// macOS returns a symlinked temp dir; resolve so the comparison is exact.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "sc-capture", "cmd", "sccap")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, CaptureDirName)
	for _, dir := range []string{root, nested} {
		t.Chdir(dir)
		if got := DefaultCaptureDir(); got != want {
			t.Errorf("from %s: got %s, want %s", dir, got, want)
		}
	}
}

func TestDefaultCaptureDirOutsideRepository(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if got, want := DefaultCaptureDir(), filepath.Join(dir, CaptureDirName); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
