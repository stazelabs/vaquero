package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-113: a web form body over the limit gets the themed page, with the
// shell's navigation and footer, not a bare one.
func TestTooLargeFormIsThemed(t *testing.T) {
	e := webtest.New(t)
	req := httptest.NewRequest(http.MethodPost, "/projects/new", strings.NewReader("x"))
	req.Host = "127.0.0.1"
	req.ContentLength = 2 << 20
	rec := httptest.NewRecorder()
	e.Server.Config.Handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<title>Request too large", "That request is too large.", "Purple Bongo", `href="/"`, "/static/"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
}
