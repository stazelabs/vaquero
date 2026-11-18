package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/middleware"
)

func TestCurrentPath(t *testing.T) {
	var got string
	h := middleware.CurrentPath(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = middleware.PathFromContext(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/workspaces/abc?tab=x", nil))
	if got != "/workspaces/abc" {
		t.Fatalf("path = %q", got)
	}
	if p := middleware.PathFromContext(context.Background()); p != "" {
		t.Fatalf("without the middleware: %q", p)
	}
}
