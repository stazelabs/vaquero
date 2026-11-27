package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// VAQ-192: a noun's sub-verb is found after leading flags, as the cli
// article promises. With no arguments for the verb, each answers its own
// verb's usage line, not the noun's.
func TestSubVerbAfterLeadingFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ticket", "--server", "x", "link"}, "vaquero ticket link TICKET"},
		{[]string{"ticket", "--json", "unlink"}, "vaquero ticket unlink TICKET"},
		{[]string{"ticket", "--agent=x", "--json", "link"}, "vaquero ticket link TICKET"},
		{[]string{"attachment", "--json", "list"}, "vaquero attachment list TICKET"},
		{[]string{"attachment", "--server", "x", "new"}, "vaquero attachment new TICKET"},
		{[]string{"attachment", "--agent", "x", "delete"}, "vaquero attachment delete ID"},
		{[]string{"import", "--server", "x", "linear"}, "vaquero import linear FILE.csv"},
	} {
		var out, errb bytes.Buffer
		if code := run(tc.args, &out, &errb); code != 2 || !strings.Contains(errb.String(), tc.want) {
			t.Errorf("%v: code %d, stderr %q, want usage %q", tc.args, code, errb.String(), tc.want)
		}
	}
}

func TestSplitVerb(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		verb string
		rest []string
	}{
		{[]string{"link", "A"}, "link", []string{"A"}},
		{[]string{"--json", "link", "A"}, "link", []string{"--json", "A"}},
		{[]string{"--server", "x", "list", "T"}, "list", []string{"--server", "x", "T"}},
		{[]string{"--server=x", "list"}, "list", []string{"--server=x"}},
		{[]string{"--", "link"}, "", []string{"--", "link"}},
		{[]string{"--json"}, "", []string{"--json"}},
		{nil, "", nil},
	} {
		verb, rest := splitVerb(tc.in)
		if verb != tc.verb || !slices.Equal(rest, tc.rest) {
			t.Errorf("%v: got %q %v, want %q %v", tc.in, verb, rest, tc.verb, tc.rest)
		}
	}
}
