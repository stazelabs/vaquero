package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger writes one line per request. Health checks and static files log at
// debug. It logs no header and no query string.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 { // nothing was written: net/http sends 200
				sw.status = http.StatusOK
			}
			level := slog.LevelInfo
			if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") {
				level = slog.LevelDebug
			}
			logger.LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("path", redactPath(r.URL.Path)),
				slog.Int("status", sw.status),
				slog.Int64("dur_ms", time.Since(start).Milliseconds()),
				slog.String("request_id", GetRequestID(r.Context())),
			)
		})
	}
}

// maxLoggedPath is the most of a path a log line keeps: the client chose
// it, and a scanner's can be long.
const maxLoggedPath = 200

// redactPath drops anything after a "?" so a query string never reaches a
// log line, and cuts the rest to maxLoggedPath characters. Callers pass
// r.URL.Path, which has none; the cut guards a caller that passes a
// RequestURI.
func redactPath(p string) string {
	path, _, _ := strings.Cut(p, "?")
	return cut(path, maxLoggedPath)
}

// RedactPath is redactPath for handlers that log a request path.
func RedactPath(p string) string { return redactPath(p) }
