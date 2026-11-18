package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostCheck(t *testing.T) {
	h := HostCheck([]string{"localhost", "127.0.0.1", "::1"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
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
