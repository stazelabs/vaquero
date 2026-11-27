package handlers_test

import (
	"html"
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

// The page that shows a new token marks itself so app.js replaces the POST
// history entry with GET /admin/tokens: a refresh then reloads the list
// instead of offering to resubmit the form (VAQ-59, ruling R3).
func TestTokenCreatedPageReplacesHistory(t *testing.T) {
	c := webtest.New(t).Client()
	r := c.Post("/admin/tokens", url.Values{"name": {"ci runner"}})
	contains(t, r.Body, `data-replace-url="/admin/tokens"`)
	if strings.Contains(c.Get("/admin/tokens").Body, "data-replace-url") {
		t.Fatal("the list page marks itself too")
	}
}

// VAQ-212: Revoke asks first, with its own words for the local token.
func TestTokenRevokeAsksFirst(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	c.Post("/admin/tokens", url.Values{"name": {"ci runner"}})
	c.Post("/admin/tokens", url.Values{"name": {"local"}})
	body := html.UnescapeString(c.Get("/admin/tokens").Body)
	contains(t, body,
		`data-confirm="Revoke ci runner? Anything using it stops working."`,
		`data-confirm="Revoke local? The CLI gets a new token the next time it starts the server."`)
}
