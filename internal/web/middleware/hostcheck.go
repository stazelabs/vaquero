package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
)

// HostCheck answers 403 to any request whose Host header (port and IPv6
// brackets removed, compared without case) is not in allowed.
//
// A local server has no sign-in, so the Host header is its defence against
// DNS rebinding: a page on another site can point its own hostname at
// 127.0.0.1 and have the browser fetch from this server, but the browser
// still sends that foreign hostname as Host.
func HostCheck(allowed []string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(allowed, hostOnly(r.Host)) {
				// The refusal comes before the request log, so it says so
				// itself. Only the Host is named (cut, since the client
				// chose it): a refusal is usually a rebinding attempt or a
				// proxy in front of a local server.
				logger.WarnContext(r.Context(), "request refused: unknown host", "host", cut(r.Host, 80),
					"method", r.Method, "path", redactPath(r.URL.Path), "request_id", GetRequestID(r.Context()))
				http.Error(w, "unknown host", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hostOnly lowercases host and strips any port and IPv6 brackets.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
}

// cut shortens s to at most n runes.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
