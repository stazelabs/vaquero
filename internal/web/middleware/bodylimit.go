package middleware

import "net/http"

// MaxBody is the largest request body the app reads. Every form is small
// text; this keeps a public POST from filling memory or temp files.
const MaxBody = 1 << 20

// BodyLimit refuses a body declared larger than limit with 413, and stops
// reading any body at that limit.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				http.Error(w, "Request too large.", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
