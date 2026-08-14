package journal

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumsFile is the conventional name, matching the existing capture manual so
// bundles from either source are checked the same way.
const SumsFile = "SHA256SUMS"

// WriteSums hashes every file in dir (except SHA256SUMS itself) and writes the
// manifest in the standard `<hex>  <path>` format.
//
// Written last, after the final flush, so its presence is itself evidence that
// the session closed cleanly. An interrupted session has no SHA256SUMS, and
// verification treats that as "interrupted", not "failed".
func WriteSums(dir string) error {
	sums, err := HashDir(dir)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Strings(names)

	tmp := filepath.Join(dir, SumsFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	for _, n := range names {
		if _, err := fmt.Fprintf(bw, "%s  %s\n", sums[n], n); err != nil {
			f.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, SumsFile))
}

// HashDir returns sha256 hex digests keyed by path relative to dir.
func HashDir(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == SumsFile || strings.HasSuffix(rel, ".tmp") {
			return nil
		}
		sum, err := HashFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sum
		return nil
	})
	return out, err
}

// HashFile returns the sha256 hex digest of one file.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReadSums parses a SHA256SUMS manifest.
func ReadSums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			// Tolerate single-space separators from other tooling.
			parts = strings.Fields(line)
			if len(parts) != 2 {
				return nil, fmt.Errorf("malformed line in %s: %q", filepath.Base(path), line)
			}
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("malformed digest in %s: %q", filepath.Base(path), parts[0])
		}
		out[strings.TrimSpace(parts[1])] = parts[0]
	}
	return out, sc.Err()
}
