package middleware

import (
	"io"
	"net/http"
	"path"
	"strings"

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
	"/api/v1/tickets/*/attachments",
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

// tooLarge writes the 413 for a body declared over the limit: the file
// message on an upload route, the API envelope on /api/v1 and /mcp, and a
// page (or, without one, a small plain page) anywhere else.
func tooLarge(w http.ResponseWriter, r *http.Request, upload bool, page http.Handler) {
	switch {
	case upload:
		api.WriteFailure(w, TooLargeUpload())
	case r.URL.Path == "/mcp" || r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/"):
		api.WriteFailure(w, api.Fail(http.StatusRequestEntityTooLarge, "too_large", "The request body is too large."))
	case page != nil:
		w.Header().Set("X-Content-Type-Options", "nosniff")
		page.ServeHTTP(w, r)
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Request too large</title></head>`+
			`<body><h1>Request too large</h1><p>That request is too large. Go back and send less.</p></body></html>`)
	}
}

// BodyLimit refuses a body declared larger than limit with 413, and stops
// reading any body at that limit. An upload route (IsUpload) gets MaxUpload
// instead, and its 413 is TooLargeUpload. page answers a web form's 413
// (the themed page); nil keeps a small plain one.
func BodyLimit(limit int64, page http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, upload := limit, IsUpload(r)
			if upload {
				n = MaxUpload
			}
			if r.ContentLength > n {
				tooLarge(w, r, upload, page)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
