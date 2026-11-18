package core_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
)

func init() {
	ops = append(ops,
		coreOp{"AddComment",
			func(c *core.Core, f fixture) error {
				_, err := c.AddComment(bg, f.Owner.ID, f.T1.ID, "Looks good.")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.AddComment(bg, f.Owner.ID, f.T1.ID, "   ")
				return err
			}},
		coreOp{"EditComment",
			func(c *core.Core, f fixture) error { return c.EditComment(bg, f.Owner.ID, f.Comment.ID, "Edited.") },
			func(c *core.Core, f fixture) error { return c.EditComment(bg, f.Owner.ID, f.Comment.ID, "") }},
		coreOp{"DeleteComment",
			func(c *core.Core, f fixture) error { return c.DeleteComment(bg, f.Owner.ID, f.Comment.ID) },
			func(c *core.Core, f fixture) error { return c.DeleteComment(bg, f.Owner.ID, "nope") }},
	)
}

func TestAddAndEditComment(t *testing.T) {
	c, d, f := newFixture(t)
	if err := c.EditComment(bg, f.Owner.ID, f.Comment.ID, "Second thoughts."); err != nil {
		t.Fatal(err)
	}
	cs, err := d.RQ.ListComments(bg, f.T1.ID)
	if err != nil || len(cs) != 1 || cs[0].Body != "Second thoughts." || cs[0].AuthorName != "Owner" {
		t.Fatalf("%+v %v", cs, err)
	}
}

func TestCommentLengthLimit(t *testing.T) {
	c, _, f := newFixture(t)
	_, err := c.AddComment(bg, f.Owner.ID, f.T1.ID, strings.Repeat("x", 20001))
	fieldErr(t, err, "body", "Keep comments under 20,000 characters.")
}

func TestOnlyAuthorEditsComment(t *testing.T) {
	c, d, f := newFixture(t)
	other, err := d.Q.CreateActor(bg, gen.CreateActorParams{ID: idgen.New(), Kind: "agent", Name: "bot", CreatedAt: "2026-11-18T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	fieldErr(t, c.EditComment(bg, other.ID, f.Comment.ID, "mine now"), "body", "You can only edit your own comments.")
	fieldErr(t, c.DeleteComment(bg, other.ID, f.Comment.ID), "body", "You can only delete your own comments.")
}

func TestDeletedCommentsAreHidden(t *testing.T) {
	c, d, f := newFixture(t)
	if err := c.DeleteComment(bg, f.Owner.ID, f.Comment.ID); err != nil {
		t.Fatal(err)
	}
	if cs, _ := d.RQ.ListComments(bg, f.T1.ID); len(cs) != 0 {
		t.Fatalf("%d comments", len(cs))
	}
	fieldErr(t, c.EditComment(bg, f.Owner.ID, f.Comment.ID, "back"), "body", "That comment was deleted.")
}
