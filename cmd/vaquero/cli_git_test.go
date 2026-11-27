package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func TestCLIFileAndCommits(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board")
	if code, _, errOut := cli(t, "commits", "VAQ-1"); code != 1 || !strings.Contains(errOut, "No repository set for this project.") {
		t.Fatalf("no repo: %d %q", code, errOut)
	}
	r := gittest.Repo(t)
	old := r.WriteCommit("docs/a.md", "# Old\n", "Start (VAQ-1)")
	r.WriteCommit("docs/a.md", "# New\nMore.\n", "Finish (VAQ-1)")
	r.Tag("v0.1.0")
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir)

	if out := mustCLI(t, "file", "vaq", "docs/a.md"); out != "# New\nMore.\n" {
		t.Fatalf("file: %q", out)
	}
	if out := mustCLI(t, "file", "VAQ", "docs/a.md", "--at", old); out != "# Old\n" {
		t.Fatalf("file at: %q", out)
	}
	var f api.File
	if err := json.Unmarshal([]byte(mustCLI(t, "file", "VAQ", "docs/a.md", "--json")), &f); err != nil || f.Blob == "" || !f.Markdown {
		t.Fatalf("%v %+v", err, f)
	}
	if code, _, errOut := cli(t, "file", "VAQ", "docs/nope.md"); code != 1 || !strings.Contains(errOut, "No file docs/nope.md on main.") {
		t.Fatalf("missing: %d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "file", "VAQ", "../x"); code != 1 || !strings.Contains(errOut, "isn't a path") {
		t.Fatalf("bad path: %d %q", code, errOut)
	}

	out := mustCLI(t, "commits", "vaq-1")
	for _, want := range []string{r.Rev("main")[:7], "Finish (VAQ-1)", "Start (VAQ-1)", "+3 −1 in 1 file", "Released in v0.1.0."} {
		if !strings.Contains(out, want) {
			t.Errorf("commits lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Finish") > strings.Index(out, "Start") {
		t.Errorf("not newest first:\n%s", out)
	}
	var tc api.TicketCommits
	if err := json.Unmarshal([]byte(mustCLI(t, "commits", "VAQ-1", "--json")), &tc); err != nil || len(tc.Commits) != 2 || tc.Released != "v0.1.0" {
		t.Fatalf("%v %+v", err, tc)
	}
	for _, args := range [][]string{{"file", "VAQ"}, {"commits"}, {"commits", "VAQ-1", "--at", "main"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}

func TestCLIFetch(t *testing.T) {
	cliEnv(t)
	if code, _, errOut := cli(t, "fetch", "VAQ"); code != 1 || !strings.Contains(errOut, "No repository set for this project.") {
		t.Fatalf("no repo: %d %q", code, errOut)
	}
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.Clone(t, bare)
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir)
	seed.WriteCommit("b", "1", "two")
	seed.Git("push", "-q", "origin", "main")

	if out := mustCLI(t, "fetch", "vaq"); !strings.Contains(out, "Fetched VAQ.") {
		t.Fatalf("fetch: %q", out)
	}
	if got := r.Git("rev-parse", "origin/main"); got != seed.Rev("main") {
		t.Fatalf("origin/main %q", got)
	}
	var res api.FetchResult
	if err := json.Unmarshal([]byte(mustCLI(t, "fetch", "VAQ", "--json")), &res); err != nil || res.LastFetchAt == nil || res.Error != "" {
		t.Fatalf("%v %+v", err, res)
	}

	r.Git("remote", "set-url", "origin", t.TempDir()+"/nowhere.git")
	if code, _, errOut := cli(t, "fetch", "VAQ"); code != 1 || !strings.Contains(errOut, "Fetch failed:") || !strings.Contains(errOut, "nowhere.git") {
		t.Fatalf("failed fetch: %d %q", code, errOut)
	}
	for _, args := range [][]string{{"fetch"}, {"fetch", "VAQ", "OTH"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}

// VAQ-164: a tag name with U+009B never reaches the terminal.
func TestCLICommitsHostileTag(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board")
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "x\n", "Start (VAQ-1)")
	r.Tag("v1\u009b2J")
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir)
	out := mustCLI(t, "commits", "VAQ-1")
	if strings.ContainsRune(out, '\u009b') || !strings.Contains(out, "Released in v12J.") {
		t.Fatalf("%q", out)
	}
}

func TestTerminalSafe(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain markdown", "# Title\n\n- a\t`b` é 日本\n", "# Title\n\n- a\t`b` é 日本\n"},
		{"crlf kept", "a\r\nb\r\n", "a\r\nb\r\n"},
		{"bare cr dropped", "a\rb\r", "ab"},
		{"cr before cr", "a\r\rb", "ab"},
		{"clear screen", "x\x1b[2Jy", "x[2Jy"},
		{"osc 52", "x\x1b]52;c;SGVsbG8=\x07y", "x]52;c;SGVsbG8=y"},
		{"osc 8", "\x1b]8;;http://e.x\x1b\\link\x1b]8;;\x1b\\", "]8;;http://e.x\\link]8;;\\"},
		{"del and csi", "a\x7fb\u009bc\u0080d\u009fe", "abcde"},
		{"other c0", "a\x00b\x07c\x08d", "abcd"},
		{"tabs and newlines", "a\tb\nc", "a\tb\nc"},
		{"just above c1", " x", " x"},
	} {
		if got := terminalSafe(tc.in); got != tc.want {
			t.Errorf("%s: terminalSafe(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestFileStripsControlsOnATerminal(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board")
	r := gittest.Repo(t)
	const raw = "ok\x1b[2J\x1b]52;c;QQ==\x07\u009bx\x7fy\ta\r\nb\rc\n"
	r.WriteCommit("docs/x.txt", raw, "Add (VAQ-1)")
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir)

	old := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = old })

	stdoutIsTerminal = func(io.Writer) bool { return true }
	out := mustCLI(t, "file", "VAQ", "docs/x.txt")
	if want := "ok[2J]52;c;QQ==xy\ta\r\nbc\n"; out != want {
		t.Fatalf("terminal: %q, want %q", out, want)
	}
	for _, bad := range []string{"\x1b", "\x07", "\u009b", "\x7f"} {
		if strings.Contains(out, bad) {
			t.Errorf("terminal output holds %q", bad)
		}
	}

	stdoutIsTerminal = func(io.Writer) bool { return false }
	if out := mustCLI(t, "file", "VAQ", "docs/x.txt"); out != raw {
		t.Fatalf("piped: %q, want %q", out, raw)
	}

	stdoutIsTerminal = func(io.Writer) bool { return true }
	var f api.File
	if err := json.Unmarshal([]byte(mustCLI(t, "file", "VAQ", "docs/x.txt", "--json")), &f); err != nil || f.Text == nil || *f.Text != raw {
		t.Fatalf("json: %v %+v", err, f)
	}
}
