package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func TestDecode(t *testing.T) {
	type in struct {
		Title string `json:"title"`
	}
	for body, want := range map[string]struct {
		status int
		msg    string
	}{
		`{"title":"x"}`:    {0, ""},
		`{"title":`:        {400, "isn't valid JSON"},
		`{"titel":"x"}`:    {400, `"titel"`},
		`{"title":"x"} {}`: {400, "one JSON object"},
		`{"title": 5}`:     {400, "title"},
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var v in
		f := api.Decode(r, &v)
		switch {
		case want.status == 0 && f != nil:
			t.Errorf("%s: %+v", body, f)
		case want.status != 0 && (f == nil || f.Status != want.status || !strings.Contains(f.Message, want.msg)):
			t.Errorf("%s: %+v", body, f)
		}
	}
	big := httptest.NewRequest("POST", "/", strings.NewReader(`{"title":"`+strings.Repeat("x", 100)+`"}`))
	w := httptest.NewRecorder()
	big.Body = http.MaxBytesReader(w, big.Body, 20)
	var v in
	if f := api.Decode(big, &v); f == nil || f.Status != 413 || f.Code != "too_large" {
		t.Fatalf("%+v", f)
	}
}

func TestFromCore(t *testing.T) {
	if f := api.FromCore(&core.Invalid{Fields: map[string]string{"title": "Enter a title."}}, nil); f.Status != 422 || f.Code != "invalid" || f.Fields["title"] == "" {
		t.Fatalf("%+v", f)
	}
	if f := api.FromCore(core.ErrNotFound, nil); f.Status != 404 || f.Code != "not_found" {
		t.Fatalf("%+v", f)
	}
	cur := func(c *core.Conflict) any { return map[string]int64{"version": c.Current.Version} }
	if f := api.FromCore(&core.Conflict{Key: "VAQ-1", Current: gen.Ticket{Version: 3}}, cur); f.Status != 409 || f.Code != "conflict" || f.Current == nil {
		t.Fatalf("%+v", f)
	}
	if f := api.FromCore(errors.New("disk on fire"), nil); f.Status != 500 || strings.Contains(f.Message, "disk") {
		t.Fatalf("internal error leaked: %+v", f)
	}
}

func TestWriteFailureShape(t *testing.T) {
	w := httptest.NewRecorder()
	api.WriteFailure(w, &api.Failure{Status: 404, Code: "not_found", Message: "No ticket VAQ-9."})
	if w.Code != 404 || w.Header().Get("Content-Type") != "application/json" || !strings.Contains(w.Body.String(), `"error": {`) {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
}
