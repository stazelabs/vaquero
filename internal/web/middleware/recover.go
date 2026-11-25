package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
)

// isAPIPath is true for /api/v1 and /mcp, whose answers are JSON.
func isAPIPath(p string) bool {
	return p == "/mcp" || strings.HasPrefix(p, "/mcp/") || p == "/api/v1" || strings.HasPrefix(p, "/api/v1/")
}

// Recover turns a handler panic into a logged 500: the API's JSON envelope under /api/v1 and /mcp, plain text elsewhere. An
// http.ErrAbortHandler panic is passed on, as net/http expects.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.ErrorContext(r.Context(), "panic", "err", v, "stack", string(debug.Stack()),
					"path", redactPath(r.URL.Path), "request_id", GetRequestID(r.Context()))
				if isAPIPath(r.URL.Path) {
					api.WriteFailure(w, api.Internal())
					return
				}
				http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
