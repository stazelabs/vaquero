// Package middleware holds the HTTP middleware chain: request ids, panic
// recovery, the Host check, the request log, security headers, the CSP
// nonce, cross-origin refusal and the body limit. The request log carries
// method, path, status, duration and request id only: never a header or a
// query string.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type ctxKey int

const requestIDKey ctxKey = 0

// RequestID tags each request with a random id, echoed in X-Request-Id.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// GetRequestID returns the request id, or "".
func GetRequestID(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey).(string)
	return s
}
