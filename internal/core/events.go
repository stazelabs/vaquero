package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// Event kinds. Payloads, all JSON objects:
//
//   - *.created: {} unless noted; ticket.created {"title"}.
//   - *.updated: {"changes": {field: {"from", "to"}}}, only changed fields;
//     ticket tags are sorted name lists.
//   - status.moved: {"name", "from": position, "to": position}.
//   - ticket.reordered: {"status": name}, a move within one column.
//   - status.deleted: {"name"}.
//   - project.deleted: {"key", "name"}, with no project id (it is gone).
//   - comment.*: {"comment_id"}.
//   - attachment.added, attachment.deleted: {"attachment_id", "name"}.
//   - link.*: {"link_id", "kind", "to": "VAQ-7", "to_id"}, the
//     target ticket by id (events from before to_id carry only the key).
//   - actor.created: {"kind", "name"}.
//   - token.created, token.revoked: {"name", "prefix"}.
//   - ticket.claimed: ClaimPayload. ticket.released: {}.
//   - ticket.claim_lapsed: {"status"}, the status the ticket was left in;
//     its actor is the claimant whose lease ran out.
//
// Every successful operation appends exactly one event, with one
// exception: Heartbeat, which only extends a lease, appends none (a busy
// agent would otherwise fill the history).
const (
	EvProjectCreated    = "project.created"
	EvProjectUpdated    = "project.updated"
	EvProjectArchived   = "project.archived"
	EvProjectRestored   = "project.restored"
	EvProjectDeleted    = "project.deleted"
	EvStatusCreated     = "status.created"
	EvStatusUpdated     = "status.updated"
	EvStatusMoved       = "status.moved"
	EvStatusDeleted     = "status.deleted"
	EvMilestoneCreated  = "milestone.created"
	EvMilestoneUpdated  = "milestone.updated"
	EvMilestoneClosed   = "milestone.closed"
	EvMilestoneReopened = "milestone.reopened"
	EvTicketCreated     = "ticket.created"
	EvTicketUpdated     = "ticket.updated"
	EvTicketReordered   = "ticket.reordered"
	EvCommentCreated    = "comment.created"
	EvCommentUpdated    = "comment.updated"
	EvCommentDeleted    = "comment.deleted"
	EvAttachmentAdded   = "attachment.added"
	EvAttachmentDeleted = "attachment.deleted"
	EvLinkCreated       = "link.created"
	EvLinkDeleted       = "link.deleted"
	EvActorCreated      = "actor.created"
	EvTokenCreated      = "token.created"
	EvTokenRevoked      = "token.revoked"
	EvTicketClaimed     = "ticket.claimed"
	EvTicketReleased    = "ticket.released"
	EvClaimLapsed       = "ticket.claim_lapsed"
)

// ClaimPayload is a ticket.claimed event's payload: the lease's end, the
// status before and after, and whether it renewed the claimant's own claim.
type ClaimPayload struct {
	Until   string `json:"until"`
	From    string `json:"from"`
	To      string `json:"to"`
	Renewed bool   `json:"renewed,omitempty"`
}

// Change is one field's value before and after an update.
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// emit appends one event inside the caller's transaction. Empty ids are
// stored as NULL; a nil payload is {}.
func (c *Core) emit(ctx context.Context, q *gen.Queries, actorID, kind, projectID, ticketID string, payload any) error {
	body := []byte("{}")
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return fmt.Errorf("event payload: %w", err)
		}
	}
	_, err := q.AppendEvent(ctx, gen.AppendEventParams{
		At:        timefmt.Format(c.Now()),
		ActorID:   actorID,
		Kind:      kind,
		ProjectID: nullString(projectID),
		TicketID:  nullString(ticketID),
		Payload:   string(body),
	})
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

// EventKinds lists every event kind core emits.
func EventKinds() []string {
	return []string{
		EvProjectCreated, EvProjectUpdated, EvProjectArchived, EvProjectRestored, EvProjectDeleted,
		EvStatusCreated, EvStatusUpdated, EvStatusMoved, EvStatusDeleted,
		EvMilestoneCreated, EvMilestoneUpdated, EvMilestoneClosed, EvMilestoneReopened,
		EvTicketCreated, EvTicketUpdated, EvTicketReordered,
		EvCommentCreated, EvCommentUpdated, EvCommentDeleted,
		EvAttachmentAdded, EvAttachmentDeleted,
		EvLinkCreated, EvLinkDeleted,
		EvActorCreated, EvTokenCreated, EvTokenRevoked,
		EvTicketClaimed, EvTicketReleased, EvClaimLapsed,
	}
}
