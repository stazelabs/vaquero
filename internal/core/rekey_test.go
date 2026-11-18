package core_test

import (
	"errors"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops, coreOp{"ChangeProjectKey",
		func(c *core.Core, f fixture) error {
			_, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "VQ")
			return err
		},
		func(c *core.Core, f fixture) error {
			_, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "OTH")
			return err
		}})
}

func TestChangeProjectKey(t *testing.T) {
	c, d, f := newFixture(t)
	p, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, " vq ")
	if err != nil || p.Key != "VQ" {
		t.Fatalf("%+v %v", p, err)
	}
	if tk, _, err := c.ResolveTicket(bg, d.RQ, core.TicketKey{Project: "VQ", Number: 1}); err != nil || tk.ID != f.T1.ID {
		t.Fatalf("VQ-1: %v", err)
	}
	if _, _, err := c.ResolveTicket(bg, d.RQ, core.TicketKey{Project: "VAQ", Number: 1}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("VAQ-1 still resolves: %v", err)
	}
	ch := lastPayload(t, c)["changes"].(map[string]any)["key"].(map[string]any)
	if ch["from"] != "VAQ" || ch["to"] != "VQ" {
		t.Fatalf("%v", ch)
	}
}

// Review Focus 4.
func TestChangeKeyRefusals(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	_, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "x")
	fieldErr(t, err, "key", "Use 2 to 10 capital letters or digits, starting with a letter.")
	_, err = c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "OTH")
	fieldErr(t, err, "key", "That key is taken.")
	_, err = c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "VAQ")
	fieldErr(t, err, "key", "That's already this project's key.")
	if eventCount(t, d) != before {
		t.Fatal("refusals wrote events")
	}
}

func TestFreedKeyReusable(t *testing.T) {
	c, _, f := newFixture(t)
	if _, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "VQ"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: "VAQ", Name: "Again"}); err != nil {
		t.Fatalf("freed key refused: %v", err)
	}
}
