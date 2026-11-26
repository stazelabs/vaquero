package templates

import (
	"strconv"
	"strings"
	"testing"
)

func diffString(ls []DiffLine) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(string(l.Op) + l.Text + "\n")
	}
	return b.String()
}

// VAQ-21: a line diff by longest common subsequence.
func TestLineDiff(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{"a\nb\nc", "a\nx\nc", " a\n-b\n+x\n c\n"},
		{"", "new", "+new\n"},
		{"old", "", "-old\n"},
		{"a\nb", "a\nb\nc", " a\n b\n+c\n"},
		{"a\nb\nc\nd", "b\nd\ne", "-a\n b\n-c\n d\n+e\n"},
	} {
		if got := diffString(LineDiff(tc.a, tc.b)); got != tc.want {
			t.Errorf("%q → %q:\n%s\nwant\n%s", tc.a, tc.b, got, tc.want)
		}
	}
}

// Long runs of unchanged lines fold to their context.
func TestLineDiffFoldsUnchanged(t *testing.T) {
	var a []string
	for i := range 40 {
		a = append(a, "line"+strconv.Itoa(i))
	}
	b := append([]string{}, a...)
	b[20] = "changed"
	got := LineDiff(strings.Join(a, "\n"), strings.Join(b, "\n"))
	if n := len(got); n > 12 {
		t.Fatalf("%d lines: %s", n, diffString(got))
	}
	s := diffString(got)
	if !strings.Contains(s, "-line20\n+changed\n") || !strings.Contains(s, " line17\n") || !strings.Contains(s, " line23\n") || strings.Contains(s, "line5\n") {
		t.Fatal(s)
	}
	if got[0].Op != '…' {
		t.Fatalf("want a fold marker first: %s", s)
	}
}

// A huge rewrite stays bounded: everything removed, then everything added.
func TestLineDiffBounded(t *testing.T) {
	var a, b []string
	for i := range 3000 {
		a = append(a, "a"+strconv.Itoa(i))
		b = append(b, "b"+strconv.Itoa(i))
	}
	got := LineDiff(strings.Join(a, "\n"), strings.Join(b, "\n"))
	if len(got) != 6000 || got[0].Op != '-' || got[5999].Op != '+' {
		t.Fatal(len(got))
	}
}
