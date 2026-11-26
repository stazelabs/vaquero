package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestOriginMiddlewareSurface(t *testing.T) {
	cases := []struct {
		path, client, want string
	}{
		{"/p/VAQ/tickets", "", "web"},
		{"/", "vaquero-cli/0.1.6", "web"},
		{"/api/v1/projects", "", "api"},
		{"/api/v1", "", "api"},
		{"/api/v1/projects", "curl/8.0", "api"},
		{"/api/v1/projects", "vaquero-cli/0.1.6", "cli"},
		{"/mcp", "vaquero-mcp/0.1.6", "mcp"},
		{"/mcp", "vaquero-cli/0.1.6", "mcp"},
		{"/apix", "", "web"},
	}
	for _, tc := range cases {
		var got core.Origin
		h := Origin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = core.OriginFrom(r.Context()) }))
		req := httptest.NewRequest("POST", tc.path, nil)
		if tc.client != "" {
			req.Header.Set("Vaquero-Client", tc.client)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got.Surface != tc.want || got.Client != tc.client {
			t.Errorf("%s with client %q: %+v, want surface %s", tc.path, tc.client, got, tc.want)
		}
	}
}

func TestOriginMiddlewareHeaders(t *testing.T) {
	var got core.Origin
	h := Origin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = core.OriginFrom(r.Context()) }))
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Vaquero-Client", " vaquero-mcp/0.1.6 ")
	req.Header.Set("Vaquero-Harness", "codex/1.2")
	req.Header.Set("Vaquero-Model", strings.Repeat("m", 100))
	req.Header.Set("Vaquero-Run", "run-l")
	h.ServeHTTP(httptest.NewRecorder(), req)
	want := core.Origin{Surface: "mcp", Client: "vaquero-mcp/0.1.6", Harness: "codex/1.2", Model: strings.Repeat("m", 100), Run: "run-l"}
	// Clipping to 80 is core's (emit), so the context keeps what was sent.
	if got != want {
		t.Fatalf("%+v, want %+v", got, want)
	}
}
