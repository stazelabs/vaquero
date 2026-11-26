package gitread_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// noControls fails when s has a C0 or C1 control character or DEL.
func noControls(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("%s: control %U in %q", what, r, s)
		}
	}
	if strings.Contains(s, "\x9b") {
		t.Fatalf("%s: raw 0x9b byte in %q", what, s)
	}
}

// I1: git's error line (a remote's sideband text among it) reaches the
// terminal, the API and the log without escape sequences.
func TestErrorLineHasNoControlCharacters(t *testing.T) {
	for _, in := range []string{
		"remote: error: \x1b]52;c;aGk=\x07copied \u009b2J cleared",
		"fatal: raw \x9b[2J byte",
		"\x1b[31m\x1b]0;title\x07\n",
	} {
		got := gitread.FirstLine(in)
		noControls(t, "FirstLine", got)
		if got == "" {
			t.Errorf("%q lost everything", in)
		}
	}
	if got := gitread.FirstLine("remote: error: \x1b]52;c;aGk=\x07copied \u009b2J"); got != "remote: error: ]52;c;aGk=copied 2J" {
		t.Fatalf("%q", got)
	}
}

// I1: a remote's URL, as shown, has no control characters either.
func TestDisplayURLHasNoControlCharacters(t *testing.T) {
	for _, in := range []string{
		"https://u:tok@host/o/\x1b]52;c;aGk=\x07r\u009b.git",
		"/srv/\x1b]52;c;aGk=\x07r.git",
		"git@host:o/\x9br.git",
	} {
		noControls(t, "DisplayURL", gitread.DisplayURL(in))
	}
	if got := gitread.DisplayURL("https://u:tok@host/o/\x1b]52;c;aGk=\x07r\u009b.git"); got != "https://host/o/]52;c;aGk=r.git" {
		t.Fatalf("%q", got)
	}
}

// I1: a path with a C1 control (U+009B is CSI to a terminal) isn't a
// path Vaquero takes or shows, any more than a C0 one.
func TestValidPathRefusesC1(t *testing.T) {
	for _, p := range []string{"docs/\u009b2J.md", "docs/\x1b]52;c;aGk=\x07.md", "a\u0085b"} {
		if gitread.ValidPath(p) {
			t.Errorf("ValidPath(%q) = true", p)
		}
	}
}

// I1: the files a Diffstat lists (a rename's new path among them) have no
// control characters.
func TestDiffstatFilesHaveNoControlCharacters(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a.md", strings.Repeat("x\n", 10), "one")
	r.Git("mv", "a.md", "\x1b]52;c;aGk=\x07a.md")
	r.Write("\u009b2J.md", "y\n")
	c := r.Commit("rename and add")
	_, g := open(t, r)
	d, err := g.Diffstat(t.Context(), []string{c})
	if err != nil || len(d.Files) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	for _, f := range d.Files {
		noControls(t, "Diffstat", f)
	}
}
