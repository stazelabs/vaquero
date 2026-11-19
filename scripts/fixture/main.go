// Command fixture writes testdata/upgrade/v{VERSION}.sqlite: a database with
// a little of everything, made through internal/core at the current schema,
// and v{VERSION}.json with its schema version and row counts. Run it (make
// fixture VERSION=X) before each release and commit both files; the test
// TestFixturesMigrateToHead then proves every released database migrates
// to head with nothing lost. Never edit or delete a fixture.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// tables are the ones whose row counts the fixture records.
var tables = []string{
	"settings", "actors", "api_tokens", "projects", "statuses", "milestones",
	"tickets", "tags", "ticket_tags", "comments", "ticket_links", "events", "mcp_calls",
}

func main() {
	version := flag.String("version", "", "the release this fixture stands for, like 0.1.0")
	dir := flag.String("dir", "testdata/upgrade", "where to write it")
	flag.Parse()
	if *version == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/fixture -version X")
		os.Exit(2)
	}
	if err := write(context.Background(), *dir, *version); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

func write(ctx context.Context, dir, version string) error {
	base := filepath.Join(dir, "v"+version)
	if _, err := os.Stat(base + ".sqlite"); err == nil {
		return fmt.Errorf("%s.sqlite exists; fixtures are never replaced", base)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "vaquero-fixture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "vaquero.db")
	w, err := appdb.Open(path)
	if err != nil {
		return err
	}
	defer w.Close()
	if err := appdb.Migrate(w); err != nil {
		return err
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	c := core.New(w, func() time.Time { return now })
	if err := seed(ctx, c, gen.New(w), now); err != nil {
		return err
	}
	schema, err := appdb.Version(w)
	if err != nil {
		return err
	}
	counts := map[string]int64{}
	for _, t := range tables {
		var n int64
		if err := w.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return fmt.Errorf("count %s: %w", t, err)
		}
		counts[t] = n
	}
	// VACUUM INTO writes one self-contained file with the WAL folded in.
	if _, err := w.ExecContext(ctx, "VACUUM INTO ?", base+".sqlite"); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]any{"version": version, "schema": schema, "counts": counts}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s.sqlite and %s.json (schema %d)\n", base, base, schema)
	return nil
}

// seed makes a little of everything the schema can hold.
func seed(ctx context.Context, c *core.Core, q *gen.Queries, now time.Time) error {
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		return err
	}
	by := owner.ID
	vaq, err := c.CreateProject(ctx, by, core.ProjectInput{Key: "VAQ", Name: "Vaquero", Description: "The tracker itself."})
	if err != nil {
		return err
	}
	old, err := c.CreateProject(ctx, by, core.ProjectInput{Key: "OLD", Name: "Old project"})
	if err != nil {
		return err
	}
	if _, err := c.CreateStatus(ctx, by, vaq.ID, "Blocked", "in_progress"); err != nil {
		return err
	}
	ms, err := c.CreateMilestone(ctx, by, vaq.ID, core.MilestoneInput{Name: "First release", DueOn: "2026-12-31"})
	if err != nil {
		return err
	}
	statuses, err := q.ListStatuses(ctx, vaq.ID)
	if err != nil {
		return err
	}
	byCategory := map[string]string{}
	for _, s := range statuses {
		if _, ok := byCategory[s.Category]; !ok {
			byCategory[s.Category] = s.ID
		}
	}
	newTicket := func(in core.TicketInput) (gen.Ticket, error) { return c.CreateTicket(ctx, by, vaq.ID, in) }
	parent, err := newTicket(core.TicketInput{Title: "Ship the first release", Body: "Everything **needed** for v0.1.0.", Priority: 2, MilestoneID: ms.ID, Tags: []string{"release"}})
	if err != nil {
		return err
	}
	child, err := newTicket(core.TicketInput{Title: "Write the install guide", ParentID: parent.ID, AssigneeID: by, Tags: []string{"docs", "release"}})
	if err != nil {
		return err
	}
	lasso, err := newTicket(core.TicketInput{Title: "Throw a lasso around the backlog", Priority: 1})
	if err != nil {
		return err
	}
	done, err := newTicket(core.TicketInput{Title: "Pick a name", StatusID: byCategory["done"]})
	if err != nil {
		return err
	}
	if _, err := newTicket(core.TicketInput{Title: "Rewrite it in another language", StatusID: byCategory["cancelled"]}); err != nil {
		return err
	}
	if _, err := newTicket(core.TicketInput{Title: "Someday: themes", StatusID: byCategory["backlog"], Priority: 4}); err != nil {
		return err
	}
	review, err := newTicket(core.TicketInput{Title: "Review the board styles", StatusID: byCategory["in_review"], Priority: 3})
	if err != nil {
		return err
	}
	if _, err := c.CreateTicket(ctx, by, old.ID, core.TicketInput{Title: "An old ticket"}); err != nil {
		return err
	}
	cm, err := c.AddComment(ctx, by, parent.ID, "Started on this.")
	if err != nil {
		return err
	}
	if err := c.EditComment(ctx, by, cm.ID, "Started on this; the checklist is in the description."); err != nil {
		return err
	}
	gone, err := c.AddComment(ctx, by, parent.ID, "A comment that was taken back.")
	if err != nil {
		return err
	}
	if err := c.DeleteComment(ctx, by, gone.ID); err != nil {
		return err
	}
	if _, err := c.AddComment(ctx, by, lasso.ID, "Lasso first, then sort."); err != nil {
		return err
	}
	if _, err := c.AddLink(ctx, by, child.ID, "blocks", core.TicketKey{Project: "VAQ", Number: parent.Number}); err != nil {
		return err
	}
	if _, err := c.AddLink(ctx, by, review.ID, "relates", core.TicketKey{Project: "VAQ", Number: parent.Number}); err != nil {
		return err
	}
	if _, err := c.AddLink(ctx, by, done.ID, "duplicates", core.TicketKey{Project: "OLD", Number: 1}); err != nil {
		return err
	}
	agent, err := c.EnsureAgent(ctx, by, "builder")
	if err != nil {
		return err
	}
	if _, err := c.Claim(ctx, agent.ID, lasso.ID, 15*time.Minute); err != nil {
		return err
	}
	_, tok, err := c.CreateToken(ctx, by, by, "laptop")
	if err != nil {
		return err
	}
	if err := c.RevokeToken(ctx, by, tok.ID); err != nil {
		return err
	}
	if _, _, err := c.CreateToken(ctx, by, by, "local"); err != nil {
		return err
	}
	// Archived last: an archived project takes no new tickets or links.
	if err := c.SetProjectArchived(ctx, by, old.ID, true); err != nil {
		return err
	}
	// The MCP log is written outside core, as the server writes it.
	return q.RecordMCPCall(ctx, gen.RecordMCPCallParams{
		ID: idgen.New(), At: timefmt.Format(now), TokenPrefix: "vq_abcdef",
		Tool: "claim_ticket", Args: `key=VAQ-3`, Outcome: "ok", DurationMs: 4,
		ActorID: nullString(agent.ID),
	})
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
