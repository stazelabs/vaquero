package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/logfile"
)

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "vaquero ") {
		t.Fatalf("%q", out.String())
	}
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: vaquero") {
		t.Fatalf("%d %q", code, out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"nope"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage") {
		t.Fatalf("%d %q", code, errb.String())
	}
}

// VAQ-61, VAQ-121: a server whose output is a regular file (the data
// directory's vaquero.log when the command line started it, or the log
// brew services opened for it) writes its log through a rotating writer;
// any other output is left alone.
func TestBackgroundLog(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"vaquero.log", "other.log"} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		w := backgroundLog(f)
		if w == nil {
			t.Fatalf("%s wasn't recognised", name)
		}
		w.Close()
	}
	r, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer pw.Close()
	if backgroundLog(pw) != nil || backgroundLog(&bytes.Buffer{}) != nil {
		t.Fatal("a pipe or a buffer was taken for a log file")
	}
}

// VAQ-121: the log brew services opened (both standard output and error,
// as the formula's log_path and error_log_path name one file) rotates at
// logfile.Max like vaquero.log, and after the rotation both descriptors
// write to the new file, so no line lands in the old one.
func TestBackgroundLogRotatesTheServiceLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "var", "log", "vaquero.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), logfile.Max-10), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	w := backgroundLog(stderr)
	if w == nil {
		t.Fatal("the service's log wasn't recognised")
	}
	defer w.Close()
	if _, err := w.Write([]byte("a line past the limit\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write([]byte("a crash trace\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "a line past the limit\na crash trace\n" {
		t.Fatalf("vaquero.log holds %q", b)
	}
	if st, err := os.Stat(path + ".1"); err != nil || st.Size() != logfile.Max-10 {
		t.Fatalf(".1: %v %v", st, err)
	}
}

// `vaquero serve 2>file` opens the file without O_APPEND, at offset 0: from
// the start, standard error writes through the log's own descriptor, so a
// crash trace lands after the log's lines instead of over them.
func TestBackgroundLogWithoutAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.log")
	stderr, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	w := backgroundLog(stderr)
	if w == nil {
		t.Fatal("not recognised")
	}
	defer w.Close()
	if _, err := w.Write([]byte("first line\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write([]byte("a crash trace\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "first line\na crash trace\n" {
		t.Fatalf("the log holds %q", b)
	}
}
