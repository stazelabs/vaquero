package handlers_test

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// gitWorld is apiWorld (VAQ with VAQ-1 to VAQ-3) with a repository set:
// README.md on main.
func gitWorld(t *testing.T) (*webtest.Env, *webtest.Client, string, *gittest.R) {
	t.Helper()
	e, c, tok := apiWorld(t)
	repo := gittest.Repo(t)
	repo.WriteCommit("README.md", "# Readme\n", "first")
	setRepo(t, e, repo.Dir)
	return e, c, tok, repo
}

func setRepo(t *testing.T, e *webtest.Env, dir string) {
	t.Helper()
	p, err := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	main := "main"
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{RepoPath: &dir, MainBranch: &main}); err != nil {
		t.Fatal(err)
	}
}

func TestFileViewRendersMarkdownSafely(t *testing.T) {
	_, c, _, repo := gitWorld(t)
	repo.WriteCommit("docs/evil.md", "# Title\n\n<script>alert(1)</script>\n\n[click](javascript:alert(2))\n", "evil")
	r := c.Get("/p/VAQ/files/docs/evil.md")
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	contains(t, r.Body, `<h1 id="doc-title">Title</h1>`, "&lt;script&gt;alert(1)&lt;/script&gt;", "docs/evil.md", "prose-vq")
	lacks(t, r.Body, "<script>alert", `href="javascript:`)

	// Anything else is escaped text in a <pre>.
	repo.WriteCommit("main.go", "package main\n\n// <b>bold</b> & more\n", "code")
	r = c.Get("/p/VAQ/files/main.go")
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	contains(t, r.Body, "<pre", "// &lt;b&gt;bold&lt;/b&gt; &amp; more")
	lacks(t, r.Body, "<b>bold</b>")
}

func TestFileViewAtRevision(t *testing.T) {
	_, c, tok, repo := gitWorld(t)
	old := repo.WriteCommit("docs/a.md", "Old words.\n", "one")
	repo.WriteCommit("docs/a.md", "New words.\n", "two")
	page := c.Get("/p/VAQ/files/docs/a.md").Body
	contains(t, page, "New words.")
	lacks(t, page, "Old words.")
	page = c.Get("/p/VAQ/files/docs/a.md?at=" + old).Body
	contains(t, page, "Old words.", old[:7], `href="/p/VAQ/files/docs/a.md"`) // a way back to main
	lacks(t, page, "New words.")

	// A missing file is a 404 page that names it.
	r := c.Get("/p/VAQ/files/docs/nope.md")
	if r.Status != 404 {
		t.Fatalf("missing: %d", r.Status)
	}
	contains(t, r.Body, "No file docs/nope.md on main.")
	if r := c.Get("/p/VAQ/files/docs/a.md?at=nobranch"); r.Status != 404 || !strings.Contains(r.Body, "No revision nobranch in the repository.") {
		t.Fatalf("missing revision: %d %s", r.Status, r.Body)
	}

	// The API.
	r = c.API("GET", "/api/v1/projects/VAQ/files/docs/a.md?at="+old, tok, nil)
	var raw map[string]any
	if err := json.Unmarshal([]byte(r.Body), &raw); err != nil || r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	for _, k := range []string{"path", "at", "blob", "size", "text", "too_large", "markdown"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("no %s: %s", k, r.Body)
		}
	}
	var f api.File
	decode(t, r.Body, &f)
	if f.Path != "docs/a.md" || f.At != old || f.Text == nil || *f.Text != "Old words.\n" || f.Size != 11 || f.TooLarge || !f.Markdown ||
		f.Blob != repo.Git("rev-parse", old+":docs/a.md") {
		t.Fatalf("%+v", f)
	}
	decode(t, c.API("GET", "/api/v1/projects/vaq/files/docs/a.md", tok, nil).Body, &f)
	if f.At != "main" || *f.Text != "New words.\n" {
		t.Fatalf("at main: %+v", f)
	}
	if r := c.API("GET", "/api/v1/projects/VAQ/files/docs/nope.md", tok, nil); r.Status != 404 || !strings.Contains(r.Body, "No file docs/nope.md on main.") {
		t.Fatalf("missing: %d %s", r.Status, r.Body)
	}

	// Past MaxShow the text is left out, on the page and in the API.
	repo.WriteCommit("big.txt", strings.Repeat("x", 600<<10), "big")
	if r := c.API("GET", "/api/v1/projects/VAQ/files/big.txt", tok, nil); !strings.Contains(r.Body, `"too_large": true`) || strings.Contains(r.Body, `"text"`) {
		t.Fatalf("too large: %.300s", r.Body)
	}
	contains(t, c.Get("/p/VAQ/files/big.txt").Body, "This file is too large to show")
}

func TestFileViewRefusesBadInput(t *testing.T) {
	e, c, tok, _ := gitWorld(t)
	rec := gittest.Record(e.Deps.Git)
	for _, path := range []string{
		"../README.md", "docs/../README.md", "%2e%2e/README.md", "-README.md", "READ%00ME.md", ".git/config",
		"README.md?at=HEAD@%7B1%7D", "README.md?at=-x", "README.md?at=main~1", "README.md?at=a%00b",
	} {
		if r := c.Get("/p/VAQ/files/" + path); r.Status != 404 || !strings.Contains(r.Body, "Page not found") {
			t.Errorf("page %s: %d", path, r.Status)
		}
		if r := c.API("GET", "/api/v1/projects/VAQ/files/"+path, tok, nil); r.Status != 422 {
			t.Errorf("api %s: %d %s", path, r.Status, r.Body)
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("git ran for bad input: %v", calls)
	}
}

// VAQ-164: a tag with U+009B reaches neither the API nor the page.
func TestTicketCommitsHostileTag(t *testing.T) {
	_, c, tok, repo := gitWorld(t)
	repo.WriteCommit("a.go", "one\n", "Start (VAQ-1)")
	repo.Tag("v9\u009b2J")
	r := c.API("GET", "/api/v1/tickets/vaq-1/commits", tok, nil)
	var tc api.TicketCommits
	decode(t, r.Body, &tc)
	if tc.Released != "v92J" || strings.ContainsRune(r.Body, '\u009b') || strings.Contains(r.Body, `\u009b`) {
		t.Fatalf("api: %q %s", tc.Released, r.Body)
	}
	frag := c.Get("/t/VAQ-1/commits").Body
	contains(t, frag, "Released in v92J")
	if strings.ContainsRune(frag, '\u009b') {
		t.Fatalf("page: %s", frag)
	}
}

func TestTicketCommits(t *testing.T) {
	e, c, tok, repo := gitWorld(t)
	repo.WriteCommit("a.go", "one\n", "Start the board (VAQ-1)")
	repo.Tag("v0.1.0")
	repo.WriteCommit("b.go", "x\n", "Another ticket (VAQ-12)")
	repo.WriteCommit("a.go", "one\ntwo\nthree\n", "Finish the board (VAQ-1)")
	repo.Tag("v0.2.0")
	repo.Tag("v0.10.0")
	repo.Branch("side")
	repo.WriteCommit("c.go", "side\n", "Not merged (VAQ-1)")
	repo.Checkout("main")

	var tc api.TicketCommits
	r := c.API("GET", "/api/v1/tickets/vaq-1/commits", tok, nil)
	decode(t, r.Body, &tc)
	if r.Status != 200 || len(tc.Commits) != 2 || tc.Commits[0].Subject != "Finish the board (VAQ-1)" || tc.Commits[1].Subject != "Start the board (VAQ-1)" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if c0 := tc.Commits[0]; c0.Hash != repo.Rev("main") || c0.Short == "" || c0.Author != gittest.AuthorName || c0.At == "" {
		t.Fatalf("%+v", c0)
	}
	if tc.Diffstat.Added != 3 || tc.Diffstat.Removed != 0 || len(tc.Diffstat.Files) != 1 || tc.Diffstat.Files[0] != "a.go" {
		t.Fatalf("diffstat %+v", tc.Diffstat)
	}
	if tc.Released != "v0.2.0" {
		t.Fatalf("released %q", tc.Released)
	}

	// A ticket with none: an empty list, no release.
	r = c.API("GET", "/api/v1/tickets/VAQ-2/commits", tok, nil)
	if r.Status != 200 || !strings.Contains(r.Body, `"commits": []`) || strings.Contains(r.Body, "released") {
		t.Fatalf("none: %d %s", r.Status, r.Body)
	}

	// The ticket page reads its commits as a fragment of its own.
	contains(t, c.Get("/t/VAQ-1").Body, `hx-get="/t/VAQ-1/commits"`, `id="commits"`)
	frag := c.Get("/t/VAQ-1/commits").Body
	contains(t, frag, "Finish the board (VAQ-1)", "Start the board (VAQ-1)", repo.Rev("main")[:7], "Released in v0.2.0", "+3", "1 file",
		`href="/p/VAQ/files/a.go"`)
	lacks(t, frag, "Not merged", "Another ticket")
	contains(t, c.Get("/t/VAQ-2/commits").Body, "No commits mention VAQ-2 on main.")

	// A ticket moved from another project keeps the commits under its old key.
	oth, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	t2, _, _ := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: 3})
	if _, err := e.Deps.Core.MoveTicketToProject(bg, e.Deps.Actor.ID, t2.ID, oth.ID, t2.Version); err != nil {
		t.Fatal(err)
	}
	repo.WriteCommit("d.go", "d\n", "Before the move (VAQ-3)")
	setRepoKey(t, e, "OTH", repo.Dir)
	repo.WriteCommit("e.go", "e\n", "After the move (OTH-1)")
	decode(t, c.API("GET", "/api/v1/tickets/OTH-1/commits", tok, nil).Body, &tc)
	if len(tc.Commits) != 2 || tc.Commits[0].Subject != "After the move (OTH-1)" || tc.Commits[1].Subject != "Before the move (VAQ-3)" {
		t.Fatalf("moved: %+v", tc.Commits)
	}
}

func setRepoKey(t *testing.T, e *webtest.Env, key, dir string) {
	t.Helper()
	p, err := e.Deps.R.GetProjectByKey(bg, key)
	if err != nil {
		t.Fatal(err)
	}
	main := "main"
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{RepoPath: &dir, MainBranch: &main}); err != nil {
		t.Fatal(err)
	}
}

func TestTicketCommitsAtMost50(t *testing.T) {
	_, c, tok, repo := gitWorld(t)
	for i := range 52 {
		repo.Git("commit", "-q", "--allow-empty", "-m", fmt.Sprintf("Step %d (VAQ-1)", i))
	}
	var tc api.TicketCommits
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1/commits", tok, nil).Body, &tc)
	if len(tc.Commits) != 50 || tc.Commits[0].Subject != "Step 51 (VAQ-1)" {
		t.Fatalf("%d %+v", len(tc.Commits), tc.Commits[:1])
	}
}

func TestRepoStatusNoNetwork(t *testing.T) {
	e, c, tok := apiWorld(t)
	bare := gittest.Bare(t)
	repo := gittest.Clone(t, bare)
	repo.WriteCommit("a", "1", "one")
	repo.Git("push", "-q", "origin", "main")
	repo.Branch("pushed")
	repo.Git("push", "-q", "origin", "pushed")
	repo.Branch("solo")
	repo.WriteCommit("b", "1", "solo work")
	repo.Checkout("pushed")
	repo.WriteCommit("c", "1", "pushed, then more")
	repo.Checkout("main")
	repo.WriteCommit("a", "2", "two")
	setRepo(t, e, repo.Dir)
	rec := gittest.Record(e.Deps.Git)

	r := c.API("GET", "/api/v1/projects/VAQ/repo/status", tok, nil)
	var st api.RepoStatus
	decode(t, r.Body, &st)
	if r.Status != 200 || st.Main.Ahead != 1 || st.Main.Behind != 0 || !st.Main.Tracked {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	want := []api.BranchStatus{{Name: "pushed", Ahead: 1, Tracked: true}, {Name: "solo", Ahead: 0, Tracked: false}}
	if len(st.UnpushedBranches) != 2 || st.UnpushedBranches[0] != want[0] || st.UnpushedBranches[1] != want[1] {
		t.Fatalf("unpushed %+v", st.UnpushedBranches)
	}
	if n := rec.Count("fetch"); n != 0 || len(rec.Calls()) == 0 {
		t.Fatalf("fetch ran %d times (%d calls)", n, len(rec.Calls()))
	}
}

func TestGitReadsNeedARepoAndGit(t *testing.T) {
	e, c, tok := apiWorld(t)
	check := func(code, msg string) {
		t.Helper()
		for _, path := range []string{"/api/v1/tickets/VAQ-1/commits", "/api/v1/projects/VAQ/files/README.md", "/api/v1/projects/VAQ/repo/status"} {
			r := c.API("GET", path, tok, nil)
			if r.Status != 409 || !strings.Contains(r.Body, `"code": "`+code+`"`) || !strings.Contains(r.Body, msg) {
				t.Errorf("%s: %d %s", path, r.Status, r.Body)
			}
		}
		page := html.EscapeString(msg)
		contains(t, c.Get("/t/VAQ-1").Body, "Commits", page)
		contains(t, c.Get("/t/VAQ-1/commits").Body, page)
		r := c.Get("/p/VAQ/files/README.md")
		if r.Status != 409 {
			t.Errorf("file page: %d", r.Status)
		}
		contains(t, r.Body, page)
	}
	check("no_repo", "No repository set for this project.")
	repo := gittest.Repo(t)
	repo.WriteCommit("README.md", "x", "one")
	setRepo(t, e, repo.Dir)
	e.Deps.Git, e.Deps.Core.Git = nil, nil
	e.Rebuild()
	check("no_git", "Git isn't installed.")
	old := gitread.NewWithVersion("git version 2.30.2")
	e.Deps.Git, e.Deps.Core.Git = old, old
	e.Rebuild()
	check("no_git", "Git 2.31 or later is needed.")
}

// read_file is GET /api/v1/projects/{key}/files/{path}, answer for answer.
func TestMCPReadFile(t *testing.T) {
	e, tok := agentEnv(t)
	repo := gittest.Repo(t)
	old := repo.WriteCommit("docs/a b.md", "# A\n", "one")
	repo.WriteCommit("docs/a b.md", "# B\n", "two")
	setRepo(t, e, repo.Dir)
	c := e.Client()
	cs := mcpConnect(t, e, tok, "builder")
	for _, tc := range []struct {
		args map[string]any
		path string
	}{
		{map[string]any{"project": "VAQ", "path": "docs/a b.md"}, "/api/v1/projects/VAQ/files/docs/a%20b.md"},
		{map[string]any{"project": "VAQ", "path": "docs/a b.md", "at": old}, "/api/v1/projects/VAQ/files/docs/a%20b.md?at=" + old},
	} {
		res, text := mcpCall(t, cs, "read_file", tc.args)
		if res.IsError {
			t.Fatalf("%v: %s", tc.args, text)
		}
		if want := c.API("GET", tc.path, tok, nil).Body; text != want {
			t.Errorf("got %s\nwant %s", text, want)
		}
		checkStructured(t, "read_file", res, text)
	}
	if res, text := mcpCall(t, cs, "read_file", map[string]any{"project": "VAQ", "path": "../x"}); !res.IsError || !strings.Contains(text, "isn't a path") {
		t.Errorf("bad path: %v %s", res.IsError, text)
	}
}

// I1: a reference's anchor lands on the heading in the file view.
func TestReferenceAnchorLands(t *testing.T) {
	e, c, _, repo := gitWorld(t)
	repo.WriteCommit("docs/board.md", "# Board\n\n## Acceptance criteria\n\nIt works.\n", "spec")
	t1, _, _ := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: 1})
	ref, err := e.Deps.Core.AddRef(bg, e.Deps.Actor.ID, t1.ID, core.RefInput{Kind: "spec", Target: "docs/board.md", Anchor: "acceptance-criteria"})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, c.Get("/t/VAQ-1").Body, `href="/p/VAQ/files/docs/board.md?ref=`+ref.ID+`#doc-acceptance-criteria"`)
	contains(t, c.Get("/p/VAQ/files/docs/board.md").Body, `<h2 id="doc-acceptance-criteria">Acceptance criteria</h2>`)
}

// M3: a main branch that's gone since the settings were saved is a 409,
// not a 500, on the API, the fragment and the file view.
func TestRepoDriftIsAConflict(t *testing.T) {
	_, c, tok, repo := gitWorld(t)
	repo.Git("branch", "-m", "main", "trunk")
	for _, path := range []string{"/api/v1/tickets/VAQ-1/commits", "/api/v1/projects/VAQ/files/README.md", "/api/v1/projects/VAQ/repo/status"} {
		if r := c.API("GET", path, tok, nil); r.Status != 409 || !strings.Contains(r.Body, `"repo_unreadable"`) {
			t.Errorf("%s: %d %s", path, r.Status, r.Body)
		}
	}
	contains(t, c.Get("/t/VAQ-1/commits").Body, "Can&#39;t read the repository at")
	if r := c.Get("/p/VAQ/files/README.md"); r.Status != 409 {
		t.Errorf("file view: %d", r.Status)
	}
}

// M6: without JavaScript the Commits section says how else to read them.
func TestCommitsNoscript(t *testing.T) {
	_, c, _, _ := gitWorld(t)
	contains(t, c.Get("/t/VAQ-1").Body, "<noscript>", "Commits need JavaScript here; run vaquero commits VAQ-1.")
}
