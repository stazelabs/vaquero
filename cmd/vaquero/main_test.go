package main

import (
	"bytes"
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
