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
	h := BodyLimit(10, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestUploadBodyLimit(t *testing.T) {
	var limit int64
	h := BodyLimit(MaxBody, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		limit = n
	}))
	// An upload route may carry MaxUpload bytes.
	req := httptest.NewRequest("POST", "/t/VAQ-1/attachments", strings.NewReader(strings.Repeat("x", MaxBody+10)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || limit != MaxBody+10 {
		t.Fatalf("upload: %d read %d", rec.Code, limit)
	}
	req = httptest.NewRequest("POST", "/api/v1/tickets/VAQ-1/attachments", strings.NewReader(strings.Repeat("x", MaxBody+10)))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || limit != MaxBody+10 {
		t.Fatalf("api upload: %d read %d", rec.Code, limit)
	}
	// Declared over MaxUpload: the API envelope with the file limit.
	req = httptest.NewRequest("POST", "/t/VAQ-1/attachments", strings.NewReader("x"))
	req.ContentLength = MaxUpload + 1
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || rec.Header().Get("Content-Type") != "application/json" ||
		!strings.Contains(rec.Body.String(), `"message": "Files can be at most 10 MB."`) {
		t.Fatalf("declared: %d %q", rec.Code, rec.Body.String())
	}
	// Other routes, and other methods on the upload path, keep MaxBody.
	for _, tc := range []struct{ method, path string }{{"POST", "/t/VAQ-1/comments"}, {"PUT", "/t/VAQ-1/attachments"}, {"POST", "/t/VAQ-1/attachments/x"}} {
		req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("x", MaxBody+1)))
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s %s: %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestTooLargeAnswersByRoute(t *testing.T) {
	h := BodyLimit(10, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, p := range []string{"/api/v1/tickets", "/mcp"} {
		req := httptest.NewRequest("POST", p, strings.NewReader("x"))
		req.ContentLength = 11
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 413 || rec.Header().Get("Content-Type") != "application/json" ||
			!strings.Contains(rec.Body.String(), `"code": "too_large"`) {
			t.Errorf("%s: %d %q %q", p, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "/t/VAQ-1/comments", strings.NewReader("x"))
	req.ContentLength = 11
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); rec.Code != 413 || !strings.HasPrefix(ct, "text/html") ||
		!strings.Contains(rec.Body.String(), "<title>") || !strings.Contains(rec.Body.String(), "too large") {
		t.Errorf("page: %d %q %q", rec.Code, ct, rec.Body.String())
	}
}
