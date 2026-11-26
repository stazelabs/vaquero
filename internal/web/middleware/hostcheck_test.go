package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

// VAQ-98: a scanner's refusals log the first few lines of each window, then
// one line with the count of the rest when the window ends.
func TestHostCheckThrottlesRefusals(t *testing.T) {
	old := hostRefusalWindow
	hostRefusalWindow = 50 * time.Millisecond
	t.Cleanup(func() { hostRefusalWindow = old })
	var mu sync.Mutex
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(lockedWriter{&mu, &buf}, nil))
	h := HostCheck([]string{"localhost"}, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	refuse := func(n int) {
		for range n {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = "evil.example"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != 403 {
				t.Fatalf("status %d", rec.Code)
			}
		}
	}
	read := func() string {
		mu.Lock()
		defer mu.Unlock()
		s := buf.String()
		buf.Reset()
		return s
	}
	refuse(20)
	if got := strings.Count(read(), "request refused: unknown host"); got != hostRefusalBurst {
		t.Fatalf("%d lines logged in the window, want %d", got, hostRefusalBurst)
	}
	deadline := time.Now().Add(2 * time.Second)
	var summary string
	for summary == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		summary = read()
	}
	if !strings.Contains(summary, "more requests refused: unknown host") || !strings.Contains(summary, "count=15") {
		t.Fatalf("summary %q", summary)
	}
	refuse(1)
	if got := read(); !strings.Contains(got, "request refused: unknown host") {
		t.Fatalf("a new window logged %q", got)
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// VAQ-98: the logged path is cut to 200 characters, in the refusal and the
// request log alike.
func TestLoggedPathsAreCut(t *testing.T) {
	long := "/" + strings.Repeat("p", 500)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	for _, h := range []http.Handler{HostCheck([]string{"localhost"}, logger)(ok), Logger(logger)(ok)} {
		buf.Reset()
		r := httptest.NewRequest("GET", long, nil)
		r.Host = "evil.example"
		h.ServeHTTP(httptest.NewRecorder(), r)
		got := buf.String()
		if !strings.Contains(got, "path=/"+strings.Repeat("p", maxLoggedPath-1)) || strings.Contains(got, strings.Repeat("p", maxLoggedPath)) {
			t.Fatalf("path not cut to %d: %s", maxLoggedPath, got)
		}
	}
}
