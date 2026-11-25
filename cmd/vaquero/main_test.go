package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// VAQ-61: a server whose output is the data directory's vaquero.log (the
// command line started it) writes its log through a rotating writer; any
// other output is left alone.
func TestBackgroundLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vaquero.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := backgroundLog(dir, f)
	if w == nil {
		t.Fatal("the data directory's log wasn't recognised")
	}
	w.Close()
	other, err := os.Create(filepath.Join(dir, "other.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if backgroundLog(dir, other) != nil || backgroundLog(dir, &bytes.Buffer{}) != nil {
		t.Fatal("another output was taken for the log")
	}
}
