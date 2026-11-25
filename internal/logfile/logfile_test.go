package logfile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/logfile"
)

func size(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// A log at the limit moves to .1 (replacing an older one); a smaller one
// stays.
func TestRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vaquero.log")
	if err := os.WriteFile(path, []byte("small"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := logfile.Rotate(path, 10); err != nil {
		t.Fatal(err)
	}
	if size(t, path) != 5 {
		t.Fatal("a small log was rotated")
	}
	if err := os.WriteFile(path+".1", []byte("oldest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 10), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := logfile.Rotate(path, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the full log stayed: %v", err)
	}
	if b, _ := os.ReadFile(path + ".1"); string(b) != strings.Repeat("x", 10) {
		t.Fatalf(".1 holds %q", b)
	}
	if err := logfile.Rotate(filepath.Join(t.TempDir(), "missing.log"), 10); err != nil {
		t.Fatalf("a missing log: %v", err)
	}
}

// A Writer moves the file to .1 when a write would take it past the limit,
// so the log and its one old file together stay near twice the limit.
func TestWriterRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vaquero.log")
	w, err := logfile.Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := []byte(strings.Repeat("a", 39) + "\n")
	for range 10 {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if n := size(t, path); n > 100 {
		t.Fatalf("log is %d bytes", n)
	}
	if n := size(t, path+".1"); n > 100 || n == 0 {
		t.Fatalf(".1 is %d bytes", n)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	left, _ := filepath.Glob(path + ".*")
	if len(left) != 1 {
		t.Fatalf("files %v, want only .1", left)
	}
}
