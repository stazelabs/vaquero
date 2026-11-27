package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// pinnedWorld is gitWorld with docs/a.md on main and a todo ticket, VAQ-4,
// with a tier and a reference to it, its plan approved.
func pinnedWorld(t *testing.T) (*webtest.Env, *webtest.Client, string, *gittest.R, gen.TicketRef) {
	t.Helper()
	e, c, tok, repo := gitWorld(t)
	repo.WriteCommit("docs/a.md", "# A\n\nOne.\n", "a")
	tk := ticket(t, e, mustProject(t, e), core.TicketInput{Title: "Build it", Tier: "light"})
	ref, err := e.Deps.Core.AddRef(bg, e.Deps.Actor.ID, tk.ID, core.RefInput{Kind: "spec", Target: "docs/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: tk.ProjectID, TicketIDs: []string{tk.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AnswerDecision(bg, e.Deps.Actor.ID, q.ID, core.Answer{Option: core.ApproveLabel}); err != nil {
		t.Fatal(err)
	}
	return e, c, tok, repo, ref
}

// O-R2: the file view opened from an approved ticket's reference says how
// the file stands against the approval, and reads its diff only when Show
// changes is opened.
func TestFileViewChangedSinceApproval(t *testing.T) {
	_, c, _, repo, ref := pinnedWorld(t)
	at := "/p/VAQ/files/docs/a.md?ref=" + ref.ID
	lacks(t, c.Get(at).Body, "data-file-since")

	repo.WriteCommit("docs/a.md", "# A\n\nTwo.\n", "change a")
	diff := "/p/VAQ/file-changes/docs/a.md?" + url.Values{"ref": {ref.ID}}.Encode()
	r := c.Get(at)
	contains(t, r.Body, "data-file-since", "Changed since approval", "Show changes", `hx-get="`+diff+`"`, `hx-trigger="toggle once"`)
	// Without ?ref=, or at a revision, there is nothing to compare with.
	lacks(t, c.Get("/p/VAQ/files/docs/a.md").Body, "data-file-since")
	lacks(t, c.Get("/p/VAQ/files/docs/a.md?at=main&ref="+ref.ID).Body, "data-file-since")
	lacks(t, c.Get("/p/VAQ/files/README.md?ref="+ref.ID).Body, "data-file-since")
	lacks(t, c.Get("/p/VAQ/files/docs/a.md?ref=nope").Body, "data-file-since")

	d := c.Get(diff)
	if d.Status != 200 {
		t.Fatalf("diff %d", d.Status)
	}
	contains(t, d.Body, "One.", "Two.", "<pre")
	lacks(t, d.Body, "<html")

	repo.WriteCommit("docs/a.md", "# A\n\nOne.\n", "back")
	repo.Rename("docs/a.md", "docs/b.md")
	repo.Commit("rename")
	contains(t, c.Get(at).Body, "Renamed to docs/b.md since approval")
	contains(t, c.Get(diff).Body, "No changes since approval.")

	repo.Remove("docs/b.md")
	repo.Commit("remove")
	contains(t, c.Get(at).Body, "Removed from main since approval")
}

// P2-R5 through the API: the ticket answer's readiness carries files, and
// a changed file makes it not ready.
func TestReadinessFilesInTheAPI(t *testing.T) {
	e, c, tok, repo, _ := pinnedWorld(t)
	workflowOn(t, e)
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-4", tok, nil).Body, &tk)
	if r := tk.Readiness; r == nil || !r.Ready || len(r.Files) != 1 || r.Files[0] != (api.ReadinessFile{Path: "docs/a.md", State: "unchanged"}) {
		t.Fatalf("readiness %+v", tk.Readiness)
	}
	repo.WriteCommit("docs/a.md", "# A\n\nTwo.\n", "change a")
	body := c.API("GET", "/api/v1/tickets/VAQ-4", tok, nil).Body
	decode(t, body, &tk)
	if r := tk.Readiness; r == nil || r.Ready || strings.Join(r.Reasons, " ") != "Changed since approval: docs/a.md. Ask again." || r.Action != "ask_again" || r.Files[0].State != "changed" {
		t.Fatalf("readiness %+v", tk.Readiness)
	}
	var ready []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/ready?project=VAQ", tok, nil).Body, &ready)
	for _, s := range ready {
		if s.Key == "VAQ-4" {
			t.Fatalf("VAQ-4 ready with a changed file: %+v", s)
		}
	}
	// Without git the files can't be checked, and it isn't ready either.
	e.Deps.Git, e.Deps.Core.Git = nil, nil
	e.Rebuild()
	tk = api.Ticket{}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-4", tok, nil).Body, &tk)
	if r := tk.Readiness; r == nil || r.Ready || strings.Join(r.Reasons, " ") != "Can't check its files: git isn't installed." || r.Action != "" {
		t.Fatalf("no git: %+v", tk.Readiness)
	}
}

// With the workflow off, nothing reads the pins: no git runs for the
// ready queue, the ticket and its list, the board, Focus or the projects
// page. (A project's own answer opens its repo to show it, workflow or
// not.)
func TestWorkflowOffRunsNoGit(t *testing.T) {
	e, c, tok, repo, _ := pinnedWorld(t)
	repo.WriteCommit("docs/a.md", "# A\n\nTwo.\n", "change a")
	rec := gittest.Record(e.Deps.Git)
	for _, path := range []string{"/api/v1/ready", "/api/v1/ready?project=VAQ", "/api/v1/tickets/VAQ-4", "/api/v1/projects/VAQ/tickets"} {
		if r := c.API("GET", path, tok, nil); r.Status != 200 || strings.Contains(r.Body, "readiness") {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
	}
	for _, path := range []string{"/", "/p/VAQ", "/p/VAQ/board", "/p/VAQ/list", "/p/VAQ/board/fragment", "/p/VAQ/focus/fragment"} {
		if r := c.Get(path); r.Status != 200 {
			t.Fatalf("%s: %d", path, r.Status)
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("%d git commands with the workflow off: %q", len(calls), calls)
	}
}

// Review I1 and M2: renamed away and replaced at its path, the file view
// says it changed, not that it was renamed; pinned against other repo
// settings (ruling M3), it says it can't be checked.
func TestFileViewReplacedAndUnchecked(t *testing.T) {
	e, c, _, repo, ref := pinnedWorld(t)
	at := "/p/VAQ/files/docs/a.md?ref=" + ref.ID
	repo.Rename("docs/a.md", "docs/b.md")
	repo.Commit("rename")
	repo.WriteCommit("docs/a.md", "# Something else\n", "a new a")
	body := c.Get(at).Body
	contains(t, body, "Changed since approval", "Show changes")
	lacks(t, body, "Renamed to")
	contains(t, c.Get("/p/VAQ/file-changes/docs/a.md?ref="+ref.ID).Body, "Something else", "One.")

	clone := gittest.Repo(t)
	clone.WriteCommit("docs/a.md", "# A\n\nOne.\n", "a")
	setRepo(t, e, clone.Dir)
	contains(t, c.Get(at).Body, "data-file-since", "check this file against the approval.")
	contains(t, c.Get("/p/VAQ/file-changes/docs/a.md?ref="+ref.ID).Body, "check this file against the approval.")
}

// Re-review m3: a pin that comes back unchecked (its commit gone after a
// history rewrite) is never "No changes since approval." in Show changes.
func TestFileChangesUncheckedPin(t *testing.T) {
	_, c, _, repo, ref := pinnedWorld(t)
	repo.Git("checkout", "-q", "--orphan", "rewritten")
	repo.Git("rm", "-q", "-r", "-f", ".")
	repo.WriteCommit("other.md", "o\n", "a new history")
	repo.Git("branch", "-M", "main")
	repo.Git("reflog", "expire", "--expire=now", "--all")
	repo.Git("gc", "-q", "--prune=now")
	body := c.Get("/p/VAQ/file-changes/docs/a.md?ref=" + ref.ID).Body
	contains(t, body, "check this file against the approval.")
	lacks(t, body, "No changes since approval.")
	contains(t, c.Get("/p/VAQ/files/docs/a.md?ref="+ref.ID).Body, "data-file-since", "check this file against the approval.")
}
