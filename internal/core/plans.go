package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// MaxPlan is the most characters a plan may hold (the schema's bound).
const MaxPlan = 200000

// PlanConflict is a plan save refused because the plan changed since the
// caller read it (or, when creating, because one already exists). Current
// is the plan as it is now.
type PlanConflict struct {
	Key     string
	Current gen.Plan
}

func (e *PlanConflict) Error() string {
	return fmt.Sprintf("The plan for %s changed since you read it; it is at revision %d.", e.Key, e.Current.Revision)
}

// SavePlan writes a ticket's plan, a versioned document kept beside the
// ticket (an epic's plan covers its children). baseRevision is the
// revision the caller read: 0 creates the plan, which must not exist yet;
// otherwise it must be the plan's current revision, else *PlanConflict
// with the current plan and nothing is written. The body is NFC-normalised
// and trimmed at its end; that text is what is stored, and what readiness
// reads. A save that changes nothing writes nothing. Every revision stays
// in plan_revisions (the current one too). The event (plan.created or
// plan.revised) carries only the revision, never the text. The ticket's own
// version is not changed.
func (c *Core) SavePlan(ctx context.Context, by, ticketID, body string, baseRevision int64) (gen.Plan, error) {
	f := fields{}
	body = checked(f, "body", strings.TrimRightFunc(norm.NFC.String(body), isSpace), MaxPlan,
		"Write the plan.", "Keep the plan under 200,000 characters.")
	if baseRevision < 0 {
		f.add("revision", "Send the revision you read, 1 or more, or leave it out to write a new plan.")
	}
	if err := f.err(); err != nil {
		return gen.Plan{}, err
	}
	var p gen.Plan
	err := c.tx(ctx, func(q *gen.Queries) error {
		t, err := q.GetTicket(ctx, ticketID)
		if err != nil {
			return notFound(err, "get ticket")
		}
		proj, err := q.GetProject(ctx, t.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		key := TicketKey{Project: proj.Key, Number: t.Number}.String()
		now := timefmt.Format(c.Now())
		p, err = q.GetPlanByTicket(ctx, t.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if baseRevision != 0 {
				return invalid("revision", key+" has no plan yet; leave out the revision to write one.")
			}
			p = gen.Plan{ID: idgen.New(), TicketID: t.ID, Revision: 1, Body: body, CreatedAt: now, UpdatedAt: now, UpdatedBy: by}
			if err := q.CreatePlan(ctx, gen.CreatePlanParams(p)); err != nil {
				return fmt.Errorf("create plan: %w", err)
			}
			if err := c.addRevision(ctx, q, p); err != nil {
				return err
			}
			return c.emit(ctx, q, by, EvPlanCreated, proj.ID, t.ID, PlanPayload{Revision: 1})
		case err != nil:
			return fmt.Errorf("get plan: %w", err)
		}
		if baseRevision != p.Revision {
			return &PlanConflict{Key: key, Current: p}
		}
		if body == p.Body {
			return nil
		}
		p.Revision++
		p.Body, p.UpdatedAt, p.UpdatedBy = body, now, by
		if err := q.UpdatePlan(ctx, gen.UpdatePlanParams{Revision: p.Revision, Body: p.Body, UpdatedAt: now, UpdatedBy: by, ID: p.ID}); err != nil {
			return fmt.Errorf("update plan: %w", err)
		}
		if err := c.addRevision(ctx, q, p); err != nil {
			return err
		}
		return c.emit(ctx, q, by, EvPlanRevised, proj.ID, t.ID, PlanPayload{Revision: p.Revision})
	})
	if err != nil {
		return gen.Plan{}, err
	}
	return p, nil
}

// addRevision keeps p's current text as its revision.
func (c *Core) addRevision(ctx context.Context, q *gen.Queries, p gen.Plan) error {
	if err := q.AddPlanRevision(ctx, gen.AddPlanRevisionParams{
		PlanID: p.ID, Revision: p.Revision, Body: p.Body, At: p.UpdatedAt, ActorID: p.UpdatedBy,
	}); err != nil {
		return fmt.Errorf("add plan revision: %w", err)
	}
	return nil
}

// isSpace is the trailing whitespace a plan loses.
func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }
