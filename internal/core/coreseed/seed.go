// Package coreseed fills a database with a little of everything the schema
// can hold, through internal/core: projects, statuses, a milestone, tickets
// with sub-tickets, tags (one renamed), a description edit, a reorder,
// comments (one edited, one deleted), links, an agent with a claim, tokens
// (one revoked), two attachments, the agent workflow on with its own tier
// map, a ticket with a tier and a model, a change with full provenance, a
// plan with two revisions, decisions (a ruling, and a group plan approval
// answered by relay, with its approvals) and an MCP log row. The upgrade fixture (scripts/fixture) and the export tests use it.
package coreseed

import (
	"bytes"
	"context"
	"database/sql"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// Seed makes a little of everything the schema can hold through c, writing
// attachment bytes to store under instance's keys. now stamps the MCP log
// row, which is written outside core as the server writes it.
func Seed(ctx context.Context, c *core.Core, q *gen.Queries, store blobs.Store, instance string, now time.Time) error {
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		return err
	}
	by := owner.ID
	vaq, err := c.CreateProject(ctx, by, core.ProjectInput{Key: "VAQ", Name: "Vaquero", Description: "The tracker itself."})
	if err != nil {
		return err
	}
	// A repo path in a reference needs the project's repository, which core
	// sets only after reading a real one; the seed records a path directly
	// (it is never read), as it writes the MCP log outside core.
	if err := q.UpdateProject(ctx, gen.UpdateProjectParams{Name: vaq.Name, Description: vaq.Description, Workflow: vaq.Workflow, TierMap: vaq.TierMap,
		RepoPath: "/seed/repo", MainBranch: "main", Remote: "", FetchMinutes: 0, UpdatedAt: vaq.UpdatedAt, ID: vaq.ID}); err != nil {
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
	// A change with its provenance, every field set, as through the MCP bridge.
	origin := core.WithOrigin(ctx, core.Origin{Surface: core.SurfaceMCP, Client: "vaquero-mcp/0.1.6", Harness: "claude-code/2.1.3", Model: "mid-model", Run: "run-1"})
	if _, err := c.AddComment(origin, by, lasso.ID, "Lasso first, then sort."); err != nil {
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
	// A project link (VAQ-119).
	if _, err := c.AddProjectLink(ctx, by, vaq.ID, "depends on", "OLD"); err != nil {
		return err
	}
	// References (VAQ-136): a repo path with an anchor and a label, and a
	// URL.
	if _, err := c.AddRef(ctx, by, parent.ID, core.RefInput{Kind: "spec", Target: "docs/release.md", Anchor: "checklist", Label: "Release checklist"}); err != nil {
		return err
	}
	if _, err := c.AddRef(ctx, by, parent.ID, core.RefInput{Kind: "design", Target: "https://example.com/designs/board"}); err != nil {
		return err
	}
	// This release's event payloads: a description edit (body_before and
	// body_after), a tag rename (ticket_ids) and a reorder.
	body := "Everything **needed** for v0.1.0, and a checklist."
	if _, err := c.PatchTicket(ctx, by, parent.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		return err
	}
	tags, err := q.ListProjectTags(ctx, vaq.ID)
	if err != nil {
		return err
	}
	for _, t := range tags {
		if t.Name == "docs" {
			if err := c.RenameTag(ctx, by, vaq.ID, t.ID, "documentation", true); err != nil {
				return err
			}
		}
	}
	if _, err := c.MoveTicket(ctx, by, child.ID, byCategory["todo"], "", parent.ID); err != nil {
		return err
	}
	// The agent workflow on VAQ, with a tier map of its own, and a tier and
	// a model on the install guide.
	on, tierMap := true, `{"claude":{"design":"big-model","light":"small-model","standard":"mid-model","heavy":"big-model"}}`
	if _, err := c.UpdateProject(ctx, by, vaq.ID, core.ProjectPatch{Workflow: &on, TierMap: &tierMap}); err != nil {
		return err
	}
	tier, model := "light", "small-model-2"
	if _, err := c.PatchTicket(ctx, by, child.ID, core.TicketPatch{Tier: &tier, Model: &model}, 0); err != nil {
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
	if err := attach(ctx, c, store, instance, by, parent.ID, "board.png", "image/png", tinyPNG); err != nil {
		return err
	}
	if err := attach(ctx, c, store, instance, agent.ID, lasso.ID, "notes.txt", "text/plain; charset=utf-8", []byte("Lasso notes.\n")); err != nil {
		return err
	}
	// Icons: VAQ an uploaded image, OLD a glyph.
	if _, err := c.SetProjectIcon(ctx, by, vaq.ID, core.IconKindImage, "", &core.IconImage{Data: tinyPNG}); err != nil {
		return err
	}
	if _, err := c.SetProjectIcon(ctx, by, old.ID, core.IconKindGlyph, "package", nil); err != nil {
		return err
	}
	// A plan with two revisions.
	if _, err := c.SavePlan(ctx, by, parent.ID, "1. Cut the release branch.", 0); err != nil {
		return err
	}
	if _, err := c.SavePlan(ctx, agent.ID, parent.ID, "1. Cut the release branch.\n2. Write the notes.", 1); err != nil {
		return err
	}
	// A ruling, and a plan approval for the epic and its child, relayed.
	if _, err := c.RecordRuling(ctx, agent.ID, core.DecisionInput{
		ProjectID: vaq.ID, TicketIDs: []string{child.ID}, RunID: "run-1", Kind: core.DecisionRuling,
		Question: "Which tier is the install guide?", Options: []core.Option{{Label: "light", Tradeoff: "Cheaper"}, {Label: "standard"}},
		Recommendation: "light", Why: "It is docs work.",
	}, core.Answer{Option: "light", Note: "Docs only."}); err != nil {
		return err
	}
	approval, err := c.AskDecision(ctx, agent.ID, core.DecisionInput{
		ProjectID: vaq.ID, TicketIDs: []string{parent.ID, child.ID}, Kind: core.DecisionApproval, Subject: "plan",
		Question: "Approve the release plan?",
	})
	if err != nil {
		return err
	}
	if _, err := c.AnswerDecision(ctx, by, approval.ID, core.Answer{Option: core.ApproveLabel, Note: "Go.", RelayedBy: agent.ID, Words: "Looks good, go."}); err != nil {
		return err
	}
	// A ticket moved from OLD to VAQ: it is VAQ-8 now, and OLD-2 still
	// leads to it (ticket_old_keys).
	moving, err := c.CreateTicket(ctx, by, old.ID, core.TicketInput{Title: "A ticket that moved"})
	if err != nil {
		return err
	}
	if _, err := c.MoveTicketToProject(ctx, by, moving.ID, vaq.ID, 0); err != nil {
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

// tinyPNG is the start of a PNG: enough for content sniffing.
var tinyPNG = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 32)...)

// attach stores data and records it on ticketID, as an upload does: bytes
// first, then the row.
func attach(ctx context.Context, c *core.Core, store blobs.Store, instance, by, ticketID, name, ctype string, data []byte) error {
	id := idgen.New()
	key := blobs.AttachmentKey(instance, id)
	if err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), ctype); err != nil {
		return err
	}
	_, err := c.AddAttachment(ctx, by, core.AttachmentInput{ID: id, TicketID: ticketID, Name: name, ContentType: ctype, BlobKey: key, Size: int64(len(data))})
	return err
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
