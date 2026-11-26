package core_test

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// pinFixture is a core with git whose project VAQ has a repo with
// docs/a.md and docs/b.md on main; T1 references both, and a URL.
func pinFixture(t *testing.T) (*core.Core, dbtest.DB, fixture, *gittest.R) {
	t.Helper()
	c, d, f := newFixture(t)
	c.Git = gitread.New()
	c.Git.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args []string, env []string, limit int) ([]byte, error) {
			if n := d.W.Stats().InUse; n != 0 {
				t.Errorf("git %v ran with %d writer connection(s) in use", args, n)
			}
			return next(ctx, dir, args, env, limit)
		}
	})
	r := gittest.Repo(t)
	r.Write("docs/a.md", "# A\n")
	r.Write("docs/b.md", "# B\n")
	r.Commit("docs")
	dir := r.Dir
	var err error
	if f.Project, err = c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{RepoPath: &dir}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"docs/a.md", "docs/b.md", "https://example.com/spec"} {
		if _, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "spec", Target: target}); err != nil {
			t.Fatal(err)
		}
	}
	return c, d, f, r
}

func askedFiles(t *testing.T, d dbtest.DB, decisionID, ticketID string) []core.FilePin {
	t.Helper()
	var raw string
	if err := d.R.QueryRow(`SELECT files FROM decision_tickets WHERE decision_id = ? AND ticket_id = ?`, decisionID, ticketID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var pins []core.FilePin
	if err := json.Unmarshal([]byte(raw), &pins); err != nil {
		t.Fatal(err)
	}
	return pins
}

// P2-R2: a plan approval pins every repo-path reference of its ticket at
// main's head, with its blob id; URL references aren't pinned. The
// approval row carries the same pins.
func TestApprovalPinsFiles(t *testing.T) {
	c, d, f, r := pinFixture(t)
	q, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	pins := askedFiles(t, d, q.ID, f.T1.ID)
	head := r.Rev("main")
	if len(pins) != 2 || pins[0].Path != "docs/a.md" || pins[1].Path != "docs/b.md" {
		t.Fatalf("pins %+v", pins)
	}
	for _, p := range pins {
		if p.Commit != head || p.Blob != r.Git("rev-parse", "main:"+p.Path) || p.RefID == "" {
			t.Fatalf("pin %+v, head %s", p, head)
		}
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := d.R.QueryRow(`SELECT pins FROM approvals WHERE decision_id = ?`, q.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var ap core.ApprovalPins
	if err := json.Unmarshal([]byte(raw), &ap); err != nil || len(ap.Files) != 2 || ap.Files[0] != pins[0] || ap.Files[1] != pins[1] {
		t.Fatalf("approval pins %s %v", raw, err)
	}
	// A question pins nothing and runs no git.
	rec := gittest.Record(c.Git)
	if _, err := c.AskDecision(bg, f.Owner.ID, core.DecisionInput{ProjectID: f.Project.ID, TicketIDs: []string{f.T1.ID}, Kind: core.DecisionQuestion, Question: "Which?"}); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.Calls()); n != 0 {
		t.Fatalf("a question ran %d git commands", n)
	}
}

// P2-R2: asking is refused when a referenced file isn't on main, or the
// repo can't be read.
func TestAskRefusesFileNotOnMain(t *testing.T) {
	c, d, f, r := pinFixture(t)
	if _, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "design", Target: "docs/x.md"}); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	_, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	fieldErr(t, err, "tickets", "docs/x.md isn't on main; merge it before asking for approval.")
	if !maps.Equal(snapshot(t, d), snap) {
		t.Fatal("a refused ask changed rows")
	}
	// On another branch only is still not on main.
	r.Branch("work")
	r.WriteCommit("docs/x.md", "x\n", "x")
	r.Checkout("main")
	_, err = c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	fieldErr(t, err, "tickets", "docs/x.md isn't on main; merge it before asking for approval.")
	// Without git the repo can't be read.
	c.Git = nil
	_, err = c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if msg := fieldMsg(err, "tickets"); len(msg) < len("Can't read the repo: ") || msg[:len("Can't read the repo: ")] != "Can't read the repo: " {
		t.Fatalf("no git: %v", err)
	}
}

// P2-R3: a commit changing a pinned file between the ask and the answer
// refuses an Approve; a commit that leaves it as it was doesn't; any other
// answer is taken.
func TestAnswerRefusesFileChangedSinceAsked(t *testing.T) {
	c, d, f, r := pinFixture(t)
	q, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	r.WriteCommit("docs/a.md", "# A, changed\n", "change a")
	snap := snapshot(t, d)
	_, err = c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"})
	fieldErr(t, err, "answer", "A file VAQ-1 references changed since this was asked; ask again.")
	if !maps.Equal(snapshot(t, d), snap) {
		t.Fatal("a refused answer changed rows")
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Request changes"}); err != nil {
		t.Fatal(err)
	}

	// A reference added between the ask and the answer is a change too
	// (P2-R4).
	q2, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "other", Target: "docs/b.md", Anchor: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatalf("a second reference to a pinned file: %v", err)
	}
	if err := c.RemoveRef(bg, f.Owner.ID, ref.ID); err != nil {
		t.Fatal(err)
	}
	q3, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	r.WriteCommit("docs/c.md", "c\n", "c")
	if _, err := c.AddRef(bg, f.Owner.ID, f.T1.ID, core.RefInput{Kind: "other", Target: "docs/c.md"}); err != nil {
		t.Fatal(err)
	}
	_, err = c.AnswerDecision(bg, f.Owner.ID, q3.ID, core.Answer{Option: "Approve"})
	fieldErr(t, err, "answer", "A file VAQ-1 references changed since this was asked; ask again.")

	// A commit that touches nothing pinned approves.
	q4, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	r.WriteCommit("docs/a.md", "# A, again\n", "change a")
	r.WriteCommit("docs/a.md", "# A, changed\n", "change it back")
	r.WriteCommit("other.md", "o\n", "elsewhere")
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q4.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
}
