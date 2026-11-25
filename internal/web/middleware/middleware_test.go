package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRequestID(t *testing.T) {
	var got string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = GetRequestID(r.Context()) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if len(got) != 16 || rec.Header().Get("X-Request-Id") != got {
		t.Fatalf("id %q, header %q", got, rec.Header().Get("X-Request-Id"))
	}
}

func TestCSPNonceMatchesContext(t *testing.T) {
	var nonce string
	h := CSP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nonce = templ.GetNonce(r.Context()) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if nonce == "" || !strings.Contains(csp, "'nonce-"+nonce+"'") {
		t.Fatalf("nonce %q not in %q", nonce, csp)
	}
	if !strings.Contains(csp, "form-action 'self';") {
		t.Fatalf("form-action must be self only: %q", csp)
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval"} {
		if strings.Contains(csp, bad) {
			t.Fatalf("CSP must not contain %s", bad)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	rec := httptest.NewRecorder()
	SecurityHeaders(false)(ok).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing baseline headers")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("no HSTS without https")
	}
	rec = httptest.NewRecorder()
	SecurityHeaders(true)(ok).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS when https")
	}
}

func TestRecover(t *testing.T) {
	var logs strings.Builder
	h := Recover(slog.New(slog.NewTextHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x?t=secret", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("code %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "boom") || strings.Contains(logs.String(), "secret") {
		t.Fatalf("log: %s", logs.String())
	}
}

func TestRecoverRepanicsAbort(t *testing.T) {
	h := Recover(quiet)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("ErrAbortHandler must be re-panicked")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

// The request log never carries a query string or a header.
func TestLoggerOmitsQueryAndHeaders(t *testing.T) {
	var logs strings.Builder
	h := Logger(slog.New(slog.NewTextHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	r := httptest.NewRequest("GET", "/signin/verify?t=secrettoken", nil)
	r.Header.Set("Cookie", "vq_session=secretcookie")
	h.ServeHTTP(httptest.NewRecorder(), r)
	out := logs.String()
	if strings.Contains(out, "secret") || !strings.Contains(out, "path=/signin/verify") || !strings.Contains(out, "status=418") {
		t.Fatalf("log: %s", out)
	}
}

func TestRedactPath(t *testing.T) {
	for in, want := range map[string]string{"/signin/verify?t=abc": "/signin/verify", "/a/b": "/a/b"} {
		if got := RedactPath(in); got != want {
			t.Errorf("RedactPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCrossOriginRefusesCrossSitePost(t *testing.T) {
	reached := false
	h := CrossOrigin(quiet)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	r := httptest.NewRequest("POST", "/signout", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || reached {
		t.Fatalf("status = %d, reached %v", w.Code, reached)
	}
	r = httptest.NewRequest("POST", "/signout", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Fatal("a same-origin POST must pass")
	}
}

// VAQ-16: a response with no write is logged as the 200 the client sees.
func TestLoggerLogsStatus200ForEmptyResponse(t *testing.T) {
	var logs strings.Builder
	h := Logger(slog.New(slog.NewTextHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(logs.String(), "status=200") {
		t.Fatalf("%d %s", rec.Code, logs.String())
	}
}
