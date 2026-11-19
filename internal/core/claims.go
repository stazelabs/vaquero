package core

import (
	"context"
	"fmt"
	"time"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// Lease lengths: a claim with lease 0 gets DefaultLease; any other lease
// must be between MinLease and MaxLease.
const (
	DefaultLease = 15 * time.Minute
	MinLease     = time.Minute
	MaxLease     = 2 * time.Hour
)

// leaseFor checks a requested lease, 0 meaning the default.
func leaseFor(d time.Duration) (time.Duration, error) {
	if d == 0 {
		return DefaultLease, nil
	}
	if d < MinLease || d > MaxLease {
		return 0, &Invalid{Fields: map[string]string{"lease": "A lease is 1 to 120 minutes."}}
	}
	return d, nil
}

// claimRefused is a refusal about the ticket itself.
func claimRefused(msg string) error { return &Invalid{Fields: map[string]string{"ticket": msg}} }

// claimed is a ticket, its key and its status, read inside a transaction.
type claimed struct {
	t      gen.Ticket
	key    string
	status gen.Status
}

func (c *Core) loadClaim(ctx context.Context, q *gen.Queries, ticketID string) (claimed, error) {
	t, err := q.GetTicket(ctx, ticketID)
	if err != nil {
		return claimed{}, notFound(err, "get ticket")
	}
	p, err := q.GetProject(ctx, t.ProjectID)
	if err != nil {
		return claimed{}, fmt.Errorf("get project: %w", err)
	}
	s, err := q.GetStatus(ctx, t.StatusID)
	if err != nil {
		return claimed{}, fmt.Errorf("get status: %w", err)
	}
	return claimed{t: t, key: TicketKey{Project: p.Key, Number: t.Number}.String(), status: s}, nil
}

// live reports whether the ticket's claim is held and unexpired at now.
func (k claimed) live(now string) bool {
	return k.t.ClaimedBy.Valid && k.t.ClaimExpiresAt.String > now
}

// holderName is the claimant's name.
func holderName(ctx context.Context, q *gen.Queries, id string) (string, error) {
	a, err := q.GetActor(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get claimant: %w", err)
	}
	return a.Name, nil
}

// untilText is a lease's end as people read it.
func untilText(stamp string) string {
	t, err := timefmt.Parse(stamp)
	if err != nil {
		return stamp
	}
	return timefmt.DisplayUTC(t)
}

// Claim gives a ticket to by for lease (0: DefaultLease): by becomes the
// assignee and the claimant, a backlog or todo ticket moves to the end of
// the project's first in-progress status, and the status it came from is
// kept so a lapsed claim can return it. Claiming one's own live claim
// renews it. Refused when another live claim holds it, when it is done or
// cancelled, or when it is assigned to someone else.
func (c *Core) Claim(ctx context.Context, by, ticketID string, lease time.Duration) (gen.Ticket, error) {
	lease, err := leaseFor(lease)
	if err != nil {
		return gen.Ticket{}, err
	}
	var out gen.Ticket
	err = c.tx(ctx, func(q *gen.Queries) error {
		k, err := c.loadClaim(ctx, q, ticketID)
		if err != nil {
			return err
		}
		now := c.Now()
		stamp := timefmt.Format(now)
		t := k.t
		if k.live(stamp) && t.ClaimedBy.String != by {
			name, err := holderName(ctx, q, t.ClaimedBy.String)
			if err != nil {
				return err
			}
			return claimRefused(fmt.Sprintf("%s is claimed by %s until %s.", k.key, name, untilText(t.ClaimExpiresAt.String)))
		}
		switch k.status.Category {
		case "done", "cancelled":
			return claimRefused(fmt.Sprintf("%s is %s.", k.key, map[string]string{"done": "done", "cancelled": "cancelled"}[k.status.Category]))
		}
		// A lapsed claim not yet swept counts as no claim, and its
		// claimant as no assignee.
		if t.AssigneeID.Valid && t.AssigneeID.String != by && !(t.ClaimedBy.Valid && t.AssigneeID.String == t.ClaimedBy.String) {
			name, err := holderName(ctx, q, t.AssigneeID.String)
			if err != nil {
				return err
			}
			return claimRefused(fmt.Sprintf("%s is assigned to %s.", k.key, name))
		}
		renew := k.live(stamp)
		// A claim already on the ticket, live or lapsed and not yet swept,
		// keeps the status the ticket was first taken from.
		prev := t.StatusID
		if t.ClaimedBy.Valid && t.ClaimPrevStatusID.Valid {
			prev = t.ClaimPrevStatusID.String
		}
		statusID, position, changedAt := t.StatusID, t.Position, t.StatusChangedAt
		target := k.status
		if k.status.Category == "backlog" || k.status.Category == "todo" {
			if target, err = q.FirstStatusInCategory(ctx, gen.FirstStatusInCategoryParams{ProjectID: t.ProjectID, Category: "in_progress"}); err != nil {
				return fmt.Errorf("first in-progress status: %w", err)
			}
			if target.ID != t.StatusID {
				max, err := q.MaxPositionInStatus(ctx, target.ID)
				if err != nil {
					return fmt.Errorf("column end: %w", err)
				}
				statusID, position, changedAt = target.ID, max+positionStep, stamp
			}
		}
		until := timefmt.Format(now.Add(lease))
		if err := q.ClaimTicket(ctx, gen.ClaimTicketParams{
			ClaimedBy: nullString(by), ClaimExpiresAt: nullString(until), ClaimPrevStatusID: nullString(prev),
			AssigneeID: nullString(by), StatusID: statusID, Position: position, StatusChangedAt: changedAt,
			UpdatedAt: stamp, ID: t.ID,
		}); err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if out, err = q.GetTicket(ctx, t.ID); err != nil {
			return fmt.Errorf("reread: %w", err)
		}
		return c.emit(ctx, q, by, EvTicketClaimed, t.ProjectID, t.ID, ClaimPayload{Until: until, From: k.status.Name, To: target.Name, Renewed: renew})
	})
	return out, err
}

// ownClaim checks that by holds a live claim on the ticket.
func (c *Core) ownClaim(ctx context.Context, q *gen.Queries, by, ticketID string) (claimed, error) {
	k, err := c.loadClaim(ctx, q, ticketID)
	if err != nil {
		return k, err
	}
	t := k.t
	switch {
	case !t.ClaimedBy.Valid:
		return k, claimRefused(k.key + " isn't claimed.")
	case t.ClaimedBy.String != by:
		name, err := holderName(ctx, q, t.ClaimedBy.String)
		if err != nil {
			return k, err
		}
		return k, claimRefused(fmt.Sprintf("%s is claimed by %s, not you.", k.key, name))
	case !k.live(timefmt.Format(c.Now())):
		return k, claimRefused(fmt.Sprintf("Your claim on %s lapsed at %s; claim it again.", k.key, untilText(t.ClaimExpiresAt.String)))
	}
	return k, nil
}

// Heartbeat extends by's live claim to now+lease (0: DefaultLease). It
// writes no event and leaves the ticket's version alone: it is a sign of
// life, not a change.
func (c *Core) Heartbeat(ctx context.Context, by, ticketID string, lease time.Duration) (gen.Ticket, error) {
	lease, err := leaseFor(lease)
	if err != nil {
		return gen.Ticket{}, err
	}
	var out gen.Ticket
	err = c.tx(ctx, func(q *gen.Queries) error {
		k, err := c.ownClaim(ctx, q, by, ticketID)
		if err != nil {
			return err
		}
		if err := q.ExtendClaim(ctx, gen.ExtendClaimParams{ClaimExpiresAt: nullString(timefmt.Format(c.Now().Add(lease))), ID: k.t.ID}); err != nil {
			return fmt.Errorf("extend claim: %w", err)
		}
		out, err = q.GetTicket(ctx, k.t.ID)
		return err
	})
	return out, err
}

// Release gives up by's live claim, keeping the ticket's status and
// assignee.
func (c *Core) Release(ctx context.Context, by, ticketID string) (gen.Ticket, error) {
	var out gen.Ticket
	err := c.tx(ctx, func(q *gen.Queries) error {
		k, err := c.ownClaim(ctx, q, by, ticketID)
		if err != nil {
			return err
		}
		if err := q.ReleaseClaim(ctx, gen.ReleaseClaimParams{UpdatedAt: timefmt.Format(c.Now()), ID: k.t.ID}); err != nil {
			return fmt.Errorf("release: %w", err)
		}
		if out, err = q.GetTicket(ctx, k.t.ID); err != nil {
			return fmt.Errorf("reread: %w", err)
		}
		return c.emit(ctx, q, by, EvTicketReleased, k.t.ProjectID, k.t.ID, nil)
	})
	return out, err
}

// ExpireClaims ends every lapsed claim, one event each with the claimant
// as actor, and returns how many. A ticket still in progress goes back to
// the status it was claimed from (or the project's first todo status, if
// that one was deleted), at the end of its column. A ticket in progress,
// todo or backlog loses its assignee if that is the claimant, so it is
// ready again. A ticket the claimant had moved on (to review, say) keeps
// its status and assignee. (Reassigning a claimed ticket, or finishing it,
// ends the claim at once: triggers in 0006_agents.sql.)
func (c *Core) ExpireClaims(ctx context.Context) (int, error) {
	n := 0
	err := c.tx(ctx, func(q *gen.Queries) error {
		stamp := timefmt.Format(c.Now())
		lapsed, err := q.ListLapsedClaims(ctx, nullString(stamp))
		if err != nil {
			return fmt.Errorf("lapsed claims: %w", err)
		}
		for _, t := range lapsed {
			s, err := q.GetStatus(ctx, t.StatusID)
			if err != nil {
				return fmt.Errorf("get status: %w", err)
			}
			statusID, position, changedAt, assignee := t.StatusID, t.Position, t.StatusChangedAt, t.AssigneeID
			to := s.Name
			if s.Category == "in_progress" {
				back, err := c.returnStatus(ctx, q, t)
				if err != nil {
					return err
				}
				if back.ID != t.StatusID {
					max, err := q.MaxPositionInStatus(ctx, back.ID)
					if err != nil {
						return fmt.Errorf("column end: %w", err)
					}
					statusID, position, changedAt = back.ID, max+positionStep, stamp
				}
				to = back.Name
			}
			// Work that hasn't reached review goes back to the queue: the
			// claimant gives up its assignment.
			if s.Category == "backlog" || s.Category == "todo" || s.Category == "in_progress" {
				if assignee.String == t.ClaimedBy.String {
					assignee = nullString("")
				}
			}
			if err := q.LapseClaim(ctx, gen.LapseClaimParams{
				StatusID: statusID, AssigneeID: assignee, Position: position, StatusChangedAt: changedAt, UpdatedAt: stamp, ID: t.ID,
			}); err != nil {
				return fmt.Errorf("lapse claim: %w", err)
			}
			if err := c.emit(ctx, q, t.ClaimedBy.String, EvClaimLapsed, t.ProjectID, t.ID, map[string]string{"status": to}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// returnStatus is where a lapsed claim puts its ticket back.
func (c *Core) returnStatus(ctx context.Context, q *gen.Queries, t gen.Ticket) (gen.Status, error) {
	if t.ClaimPrevStatusID.Valid {
		s, err := q.GetStatus(ctx, t.ClaimPrevStatusID.String)
		if err == nil {
			return s, nil
		}
	}
	s, err := q.FirstStatusInCategory(ctx, gen.FirstStatusInCategoryParams{ProjectID: t.ProjectID, Category: "todo"})
	if err != nil {
		return s, fmt.Errorf("first todo status: %w", err)
	}
	return s, nil
}
