package handlers_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestAdminHome(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	contains(t, c.Get("/").Body, `href="/admin"`)
	page := c.Get("/admin").Body
	contains(t, page, version.Version, e.Deps.Cfg.DataDir, "Schema version", `href="/admin/tokens"`)
}

var tokenRe = regexp.MustCompile(`vq_[A-Za-z0-9_-]{43}`)

func TestTokensPageCreateShowsOnce(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	r := c.Post("/admin/tokens", url.Values{"name": {"ci runner"}})
	if r.Status != 201 {
		t.Fatalf("%d", r.Status)
	}
	raw := tokenRe.FindString(r.Body)
	if raw == "" {
		t.Fatal("token not shown")
	}
	contains(t, r.Body, "Copy this token now. It won")
	list := c.Get("/admin/tokens").Body
	contains(t, list, "ci runner", raw[:9])
	if strings.Contains(list, raw) {
		t.Fatal("token shown again")
	}
	if r := c.API("GET", "/api/v1/me", raw, nil); r.Status != 200 {
		t.Fatalf("new token refused: %d", r.Status)
	}
	if r := c.Post("/admin/tokens", url.Values{"name": {" "}}); r.Status != 422 || !strings.Contains(r.Body, "Name the token.") {
		t.Fatalf("%d", r.Status)
	}
}

func TestTokenRevokeStopsAPI(t *testing.T) {
	e := webtest.New(t)
	raw := e.Token(t)
	toks, _ := e.Deps.R.ListTokens(bg)
	c := e.Client()
	r := c.Post("/admin/tokens/"+toks[0].ID+"/revoke", nil)
	contains(t, c.Follow(r).Body, "Token revoked.", "Revoked")
	if r := c.API("GET", "/api/v1/me", raw, nil); r.Status != 401 {
		t.Fatalf("%d", r.Status)
	}
}
