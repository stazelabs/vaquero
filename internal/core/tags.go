package core

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// Tag management. Tags are shared by every project (a tag is one row,
// matched by SameTag), but a project manages only its own use of them:
// renaming, merging or deleting a tag in one project changes that project's
// tickets and leaves every other project's tickets as they were. The one
// exception is a change of case or normalisation alone (bug → Bug), which
// respells the shared tag in place, since two tags that differ only so are
// one tag. A tag left on no ticket is removed.
//
// Each operation raises the version of every ticket whose tags it changes,
// so a stale edit form is refused rather than putting the old tag back, and
// appends one project event: tag.renamed, tag.merged or tag.deleted.

// TagPayload is a tag event's payload. Renamed and merged carry From and To
// (To is the tag the tickets now carry, in its own spelling); deleted
// carries Name. TicketIDs are the project's tickets whose tags changed (by
// number, read before any row moves; a ticket that had both tags of a
// merge is named once), and Tickets is how many there are.
type TagPayload struct {
	From      string   `json:"from,omitempty"`
	To        string   `json:"to,omitempty"`
	Name      string   `json:"name,omitempty"`
	Tickets   int64    `json:"tickets"`
	TicketIDs []string `json:"ticket_ids"`
}

// tagName checks a new tag name: trimmed, in NFC, 1 to 40 characters, no
// control characters and no comma (ticket forms split tags on commas).
func tagName(f fields, name string) string {
	name = norm.NFC.String(text(f, "name", name, 40, "Enter a name.", "Keep the name under 40 characters."))
	if strings.Contains(name, ",") {
		f.add("name", "Leave commas out of tag names.")
	}
	return name
}

// projectTag is the tag tagID and the project's tickets that carry it; a
// tag the project's tickets don't carry is ErrNotFound.
func projectTag(ctx context.Context, q *gen.Queries, projectID, tagID string) (gen.Tag, []string, error) {
	t, err := q.GetTag(ctx, tagID)
	if err != nil {
		return t, nil, notFound(err, "get tag")
	}
	ids, err := q.ListProjectTaggedTicketIDs(ctx, gen.ListProjectTaggedTicketIDsParams{TagID: tagID, ProjectID: projectID})
	if err != nil {
		return t, nil, fmt.Errorf("tagged tickets: %w", err)
	}
	if len(ids) == 0 {
		return t, nil, ErrNotFound
	}
	return t, ids, nil
}

// tagPayload is the payload for a change to the tickets ids.
func tagPayload(from, to, name string, ids []string) TagPayload {
	return TagPayload{From: from, To: to, Name: name, Tickets: int64(len(ids)), TicketIDs: ids}
}

// touchTagged raises the version of the project's tickets tagged tagID.
func (c *Core) touchTagged(ctx context.Context, q *gen.Queries, projectID, tagID string) error {
	err := q.TouchTaggedTickets(ctx, gen.TouchTaggedTicketsParams{UpdatedAt: timefmt.Format(c.Now()), ProjectID: projectID, TagID: tagID})
	if err != nil {
		return fmt.Errorf("touch tagged tickets: %w", err)
	}
	return nil
}

// moveTag retags the project's tickets from one tag to another, one row a
// ticket, and drops the old tag if nothing carries it any more.
func (c *Core) moveTag(ctx context.Context, q *gen.Queries, projectID, fromID, toID string) error {
	if err := c.touchTagged(ctx, q, projectID, fromID); err != nil {
		return err
	}
	if err := q.CopyProjectTicketTags(ctx, gen.CopyProjectTicketTagsParams{ToID: toID, FromID: fromID, ProjectID: projectID}); err != nil {
		return fmt.Errorf("copy tags: %w", err)
	}
	return c.dropTag(ctx, q, projectID, fromID)
}

// dropTag takes the tag off the project's tickets and removes it when no
// other ticket carries it.
func (c *Core) dropTag(ctx context.Context, q *gen.Queries, projectID, tagID string) error {
	if err := q.RemoveProjectTicketTags(ctx, gen.RemoveProjectTicketTagsParams{TagID: tagID, ProjectID: projectID}); err != nil {
		return fmt.Errorf("remove tags: %w", err)
	}
	if err := q.DeleteUnusedTag(ctx, tagID); err != nil {
		return fmt.Errorf("delete unused tag: %w", err)
	}
	return nil
}

// RenameTag renames a tag in a project. When another tag of the project
// already has the name (by SameTag) the two are merged, as MergeTag does.
// The same name again changes nothing and writes nothing.
func (c *Core) RenameTag(ctx context.Context, by, projectID, tagID, name string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		f := fields{}
		name = tagName(f, name)
		tag, ids, err := projectTag(ctx, q, projectID, tagID)
		if err != nil {
			return err
		}
		if err := f.err(); err != nil {
			return err
		}
		if name == tag.Name {
			return nil
		}
		all, err := q.ListTags(ctx)
		if err != nil {
			return fmt.Errorf("list tags: %w", err)
		}
		for _, other := range all {
			if other.ID == tag.ID || !SameTag(other.Name, name) {
				continue
			}
			m, err := q.CountProjectTagTickets(ctx, gen.CountProjectTagTicketsParams{TagID: other.ID, ProjectID: projectID})
			if err != nil {
				return fmt.Errorf("count tag: %w", err)
			}
			if m > 0 {
				return c.merge(ctx, q, by, projectID, tag, other, ids)
			}
			// Only other projects use that name: this project's tickets
			// join it, which is a rename here.
			if err := c.moveTag(ctx, q, projectID, tag.ID, other.ID); err != nil {
				return err
			}
			return c.emit(ctx, q, by, EvTagRenamed, projectID, "", tagPayload(tag.Name, other.Name, "", ids))
		}
		total, err := q.CountTagTickets(ctx, tag.ID)
		if err != nil {
			return fmt.Errorf("count tag: %w", err)
		}
		if total == int64(len(ids)) || SameTag(tag.Name, name) {
			// No other project uses it, or only the spelling changes:
			// rename the tag itself.
			if err := c.touchTagged(ctx, q, projectID, tag.ID); err != nil {
				return err
			}
			if err := q.SetTagName(ctx, gen.SetTagNameParams{Name: name, ID: tag.ID}); err != nil {
				return fmt.Errorf("rename tag: %w", err)
			}
		} else {
			// Other projects keep the old name: this one gets a new tag.
			nt, err := q.UpsertTag(ctx, gen.UpsertTagParams{ID: idgen.New(), Name: name, CreatedAt: timefmt.Format(c.Now())})
			if err != nil {
				return fmt.Errorf("create tag: %w", err)
			}
			if err := c.moveTag(ctx, q, projectID, tag.ID, nt.ID); err != nil {
				return err
			}
		}
		return c.emit(ctx, q, by, EvTagRenamed, projectID, "", tagPayload(tag.Name, name, "", ids))
	})
}

// MergeTag retags every ticket of the project tagged fromID with intoID
// instead; a ticket that had both keeps one. Other projects keep fromID.
func (c *Core) MergeTag(ctx context.Context, by, projectID, fromID, intoID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		from, ids, err := projectTag(ctx, q, projectID, fromID)
		if err != nil {
			return err
		}
		if fromID == intoID {
			return invalid("name", "Choose another tag.")
		}
		into, err := q.GetTag(ctx, intoID)
		if err != nil {
			return notFound(err, "get tag")
		}
		return c.merge(ctx, q, by, projectID, from, into, ids)
	})
}

func (c *Core) merge(ctx context.Context, q *gen.Queries, by, projectID string, from, into gen.Tag, ids []string) error {
	if err := c.moveTag(ctx, q, projectID, from.ID, into.ID); err != nil {
		return err
	}
	return c.emit(ctx, q, by, EvTagMerged, projectID, "", tagPayload(from.Name, into.Name, "", ids))
}

// DeleteTag takes a tag off every ticket of the project. Other projects
// keep it.
func (c *Core) DeleteTag(ctx context.Context, by, projectID, tagID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		tag, ids, err := projectTag(ctx, q, projectID, tagID)
		if err != nil {
			return err
		}
		if err := c.touchTagged(ctx, q, projectID, tag.ID); err != nil {
			return err
		}
		if err := c.dropTag(ctx, q, projectID, tag.ID); err != nil {
			return err
		}
		return c.emit(ctx, q, by, EvTagDeleted, projectID, "", tagPayload("", "", tag.Name, ids))
	})
}
