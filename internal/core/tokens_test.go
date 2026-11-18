package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops,
		coreOp{"CreateToken",
			func(c *core.Core, f fixture) error {
				_, _, err := c.CreateToken(bg, f.Owner.ID, f.Owner.ID, "ci")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, _, err := c.CreateToken(bg, f.Owner.ID, f.Owner.ID, " ")
				return err
			}},
		coreOp{"RevokeToken",
			func(c *core.Core, f fixture) error { return c.RevokeToken(bg, f.Owner.ID, f.Token.ID) },
			func(c *core.Core, f fixture) error { return c.RevokeToken(bg, f.Owner.ID, "nope") }},
	)
}

func TestCreateAndRevokeToken(t *testing.T) {
	c, d, f := newFixture(t)
	raw, tok, err := c.CreateToken(bg, f.Owner.ID, f.Owner.ID, "scripts")
	if err != nil || !apitoken.Valid(raw) || tok.Prefix != apitoken.Prefix(raw) {
		t.Fatalf("%v %+v", err, tok)
	}
	got, err := d.RQ.GetTokenByHash(bg, apitoken.Hash(raw))
	if err != nil || got.ID != tok.ID || got.ActorID != f.Owner.ID {
		t.Fatalf("%+v %v", got, err)
	}
	var stored int
	d.R.QueryRow(`SELECT count(*) FROM api_tokens WHERE token_hash = ? OR prefix = ?`, raw, raw).Scan(&stored)
	if stored != 0 {
		t.Fatal("raw token stored")
	}
	if err := c.RevokeToken(bg, f.Owner.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.RQ.GetToken(bg, tok.ID); !got.RevokedAt.Valid {
		t.Fatal("not revoked")
	}
	fieldErr(t, c.RevokeToken(bg, f.Owner.ID, tok.ID), "token", "That token is already revoked.")
}
