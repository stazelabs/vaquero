package handlers_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func decode(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("%v in %q", err, body)
	}
}

// Review Focus 2.
func TestAPIRefusesBadTokens(t *testing.T) {
	e := webtest.New(t)
	var logs bytes.Buffer
	e.Deps.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.Rebuild()
	good := e.Token(t)
	revoked := e.Token(t)
	toks, _ := e.Deps.R.ListTokens(bg)
	for _, tk := range toks {
		if strings.HasPrefix(revoked, tk.Prefix) {
			if err := e.Deps.Core.RevokeToken(bg, e.Deps.Actor.ID, tk.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	c := e.Client()
	var first string
	for name, tok := range map[string]string{"none": "", "malformed": "x", "unknown": "vq_" + strings.Repeat("A", 43), "revoked": revoked} {
		r := c.API("GET", "/api/v1/me", tok, nil)
		if r.Status != 401 || r.Header.Get("WWW-Authenticate") != `Bearer realm="api", error="invalid_token"` {
			t.Errorf("%s: %d %q", name, r.Status, r.Header.Get("WWW-Authenticate"))
		}
		if first == "" {
			first = r.Body
		} else if r.Body != first {
			t.Errorf("%s: body differs: %q vs %q", name, r.Body, first)
		}
	}
	if r := c.API("GET", "/api/v1/me", good, nil); r.Status != 200 {
		t.Fatalf("good token: %d", r.Status)
	}
	if strings.Contains(logs.String(), "vq_") {
		t.Fatal("a token reached the log")
	}
}

func TestAPIMe(t *testing.T) {
	e := webtest.New(t)
	r := e.Client().API("GET", "/api/v1/me", e.Token(t), nil)
	var me struct {
		Actor   struct{ Name, Kind string }
		Version string
	}
	decode(t, r.Body, &me)
	if me.Actor.Name != "Owner" || me.Actor.Kind != "human" || me.Version != version.Version {
		t.Fatalf("%+v", me)
	}
}

func TestAPIHeaders(t *testing.T) {
	e := webtest.New(t)
	r := e.Client().API("GET", "/api/v1/me", e.Token(t), nil)
	if r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("Vaquero-Version") != version.Version ||
		r.Header.Get("Vaquero-Min-Client") == "" || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%v", r.Header)
	}
}

func TestAPINotFoundJSON(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	tok := e.Token(t)
	for method, path := range map[string]string{"GET": "/api/v1/nope", "DELETE": "/api/v1/me"} {
		r := c.API(method, path, tok, nil)
		var body struct{ Error struct{ Code string } }
		decode(t, r.Body, &body)
		if r.Status < 400 || body.Error.Code == "" {
			t.Errorf("%s %s: %d %q", method, path, r.Status, r.Body)
		}
	}
}

func TestTouchThrottled(t *testing.T) {
	e := webtest.New(t)
	tok := e.Token(t)
	lastUsed := func() string {
		toks, _ := e.Deps.R.ListTokens(bg)
		for _, tk := range toks {
			if strings.HasPrefix(tok, tk.Prefix) {
				return tk.LastUsedAt.String
			}
		}
		return ""
	}
	at := func(d time.Duration) {
		e.Deps.Now = func() time.Time { return webtest.Start.Add(d) }
		e.Rebuild()
		e.Client().API("GET", "/api/v1/me", tok, nil)
	}
	at(0)
	first := lastUsed()
	at(time.Minute)
	if lastUsed() != first {
		t.Fatal("touched again within the window")
	}
	at(6 * time.Minute)
	if lastUsed() == first {
		t.Fatal("not touched after the window")
	}
}
