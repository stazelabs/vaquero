package middleware

import (
	"net/http"
	"path"

	"github.com/stazelabs/vaquero/internal/api"
)

// MaxBody is the largest request body the app reads. Every form is small
// text; this keeps a public POST from filling memory or temp files.
const MaxBody = 1 << 20

// MaxUpload is the body limit of an upload route: one 10 MB file
// (core.MaxAttachment) plus room for the multipart framing.
const MaxUpload = 10<<20 + 64<<10

// uploadRoutes are the POST paths (path.Match patterns) whose body may be
// MaxUpload bytes. Every other request keeps MaxBody.
var uploadRoutes = []string{
	"/t/*/attachments",
}

// TooLargeUpload is an upload route's 413: the API's failure envelope, which
// the web upload and the API answer alike.
func TooLargeUpload() *api.Failure {
	return api.Fail(http.StatusRequestEntityTooLarge, "too_large", "Files can be at most 10 MB.")
}

// IsUpload reports whether r is a POST to one of uploadRoutes (the web
// upload; the API upload joins it).
func IsUpload(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	for _, p := range uploadRoutes {
		if ok, _ := path.Match(p, r.URL.Path); ok {
			return true
		}
	}
	return false
}

// BodyLimit refuses a body declared larger than limit with 413, and stops
// reading any body at that limit. An upload route (IsUpload) gets MaxUpload
// instead, and its 413 is TooLargeUpload.
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, upload := limit, IsUpload(r)
			if upload {
				n = MaxUpload
			}
			if r.ContentLength > n {
				if upload {
					api.WriteFailure(w, TooLargeUpload())
				} else {
					http.Error(w, "Request too large.", http.StatusRequestEntityTooLarge)
				}
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
