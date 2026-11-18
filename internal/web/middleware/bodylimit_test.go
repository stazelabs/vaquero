package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimit(t *testing.T) {
	var read int
	h := BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		read = len(b)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 11))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared too large: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 50)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)
	if read > 10 {
		t.Fatalf("read %d bytes past the limit", read)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("ok")))
	if rec.Code != 200 || read != 2 {
		t.Fatalf("small body: %d, read %d", rec.Code, read)
	}
}
