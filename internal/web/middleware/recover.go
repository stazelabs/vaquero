package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns a handler panic into a logged plain-text 500. An
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
				http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
