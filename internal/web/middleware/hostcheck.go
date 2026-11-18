package middleware

import (
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
func HostCheck(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(allowed, hostOnly(r.Host)) {
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
