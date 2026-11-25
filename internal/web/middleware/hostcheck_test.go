package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostCheck(t *testing.T) {
	h := HostCheck([]string{"localhost", "127.0.0.1", "::1"}, slog.New(slog.DiscardHandler))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for host, want := range map[string]int{
		"localhost":         200,
		"LOCALHOST:7878":    200,
		"127.0.0.1":         200,
		"127.0.0.1:7878":    200,
		"[::1]:7878":        200,
		"[::1]":             200,
		"evil.example":      403,
		"evil.example:7878": 403,
		"127.0.0.1.nip.io":  403,
		"localhost.evil":    403,
		"":                  403,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("Host %q: %d, want %d", host, rec.Code, want)
		}
	}
}

// VAQ-15: a refused Host is logged with the Host cut to 80 characters and no
// other header.
func TestHostCheckLogsRefusal(t *testing.T) {
	var buf bytes.Buffer
	h := HostCheck([]string{"localhost"}, slog.New(slog.NewTextHandler(&buf, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("GET", "/p/VAQ/board?secret=1", nil)
	r.Host = strings.Repeat("a", 200)
	r.Header.Set("Authorization", "Bearer vq_topsecret")
	r.Header.Set("Cookie", "vq_flash=x")
	h.ServeHTTP(httptest.NewRecorder(), r)
	got := buf.String()
	if !strings.Contains(got, "level=WARN") || !strings.Contains(got, "host="+strings.Repeat("a", 80)+" ") || strings.Contains(got, strings.Repeat("a", 81)) {
		t.Fatalf("host not logged cut to 80: %s", got)
	}
	for _, leak := range []string{"vq_topsecret", "vq_flash", "secret=1"} {
		if strings.Contains(got, leak) {
			t.Fatalf("log leaks %q: %s", leak, got)
		}
	}
	buf.Reset()
	ok := httptest.NewRequest("GET", "/", nil)
	ok.Host = "localhost:7878"
	h.ServeHTTP(httptest.NewRecorder(), ok)
	if buf.Len() != 0 {
		t.Fatalf("an allowed host logged: %s", buf.String())
	}
}
