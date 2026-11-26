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

// The icon routes are uploads with their own, smaller limit: the API's
// answers in the envelope with the icon message, the web form's with the
// page.
func TestIconUploadBodyLimit(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	})
	var read int64
	h := BodyLimit(MaxBody, page)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, _ = io.Copy(io.Discard, r.Body)
	}))
	for _, tc := range []struct{ method, path string }{{"PUT", "/api/v1/projects/VAQ/icon"}, {"POST", "/p/VAQ/icon"}} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("x"))
		if !IsUpload(req) {
			t.Errorf("%s %s isn't an upload", tc.method, tc.path)
		}
		// Within the limit: read whole.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("x", 200<<10))))
		if rec.Code != 200 || read != 200<<10 {
			t.Fatalf("%s: %d read %d", tc.path, rec.Code, read)
		}
		// Undeclared and over: reading stops at the limit.
		req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("x", MaxIconUpload+10)))
		req.ContentLength = -1
		h.ServeHTTP(httptest.NewRecorder(), req)
		if read > MaxIconUpload {
			t.Fatalf("%s: read %d past the limit", tc.path, read)
		}
	}
	req := httptest.NewRequest("PUT", "/api/v1/projects/VAQ/icon", strings.NewReader("x"))
	req.ContentLength = MaxIconUpload + 1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"message": "Use an image of 256 KB or less."`) {
		t.Fatalf("api declared: %d %q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("POST", "/p/VAQ/icon", strings.NewReader("x"))
	req.ContentLength = MaxIconUpload + 1
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("web declared: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// Other methods on the icon paths are not uploads.
	for _, tc := range []struct{ method, path string }{{"POST", "/api/v1/projects/VAQ/icon"}, {"GET", "/p/VAQ/icon"}, {"PUT", "/p/VAQ/icon"}} {
		if IsUpload(httptest.NewRequest(tc.method, tc.path, nil)) {
			t.Errorf("%s %s is an upload", tc.method, tc.path)
		}
	}
}
