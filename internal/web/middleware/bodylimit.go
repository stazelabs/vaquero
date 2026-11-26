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

// MaxUpload is the body limit of an attachment upload route: one 10 MB
// file (core.MaxAttachment) plus room for the multipart framing.
const MaxUpload = 10<<20 + 64<<10

// MaxIconUpload is the body limit of a project icon upload: one 256 KB
// image (core.MaxIcon) plus room for the multipart framing and the form's
// other fields.
const MaxIconUpload = 256<<10 + 64<<10

// upload is one upload route: its method and path (a path.Match pattern),
// its body limit, the message of its 413, and whether a declared body over
// the limit gets the web page (a form post) rather than the API envelope.
type upload struct {
	method, pattern string
	limit           int64
	message         string
	page            bool
}

// uploadRoutes are the routes whose body limit isn't MaxBody: attachments
// may be larger, icons are smaller.
var uploadRoutes = []upload{
	{http.MethodPost, "/t/*/attachments", MaxUpload, "Files can be at most 10 MB.", false},
	{http.MethodPost, "/api/v1/tickets/*/attachments", MaxUpload, "Files can be at most 10 MB.", false},
	{http.MethodPost, "/p/*/icon", MaxIconUpload, IconTooLargeMessage, true},
	{http.MethodPut, "/api/v1/projects/*/icon", MaxIconUpload, IconTooLargeMessage, false},
}

// IconTooLargeMessage is the refusal of an icon image over core.MaxIcon.
const IconTooLargeMessage = "Use an image of 256 KB or less."

// TooLargeUpload is an attachment upload route's 413: the API's failure
// envelope, which the web upload and the API answer alike.
func TooLargeUpload() *api.Failure {
	return api.Fail(http.StatusRequestEntityTooLarge, "too_large", "Files can be at most 10 MB.")
}

// TooLargeIcon is an icon upload's 413 in the API envelope.
func TooLargeIcon() *api.Failure {
	return api.Fail(http.StatusRequestEntityTooLarge, "too_large", IconTooLargeMessage)
}

// uploadRoute is the upload route r is a request to, if any.
func uploadRoute(r *http.Request) (upload, bool) {
	for _, u := range uploadRoutes {
		if r.Method != u.method {
			continue
		}
		if ok, _ := path.Match(u.pattern, r.URL.Path); ok {
			return u, true
		}
	}
	return upload{}, false
}

// IsUpload reports whether r is a request to one of uploadRoutes: the
// attachment uploads (web and API) and the icon uploads (web and API).
func IsUpload(r *http.Request) bool {
	_, ok := uploadRoute(r)
	return ok
}

// tooLarge writes the 413 for a body declared over the limit: the route's
// message in the API envelope on an upload route (unless it is a form
// post), the API envelope on /api/v1 and /mcp, and a page (or, without
// one, a small plain page) anywhere else.
func tooLarge(w http.ResponseWriter, r *http.Request, up *upload, page http.Handler) {
	switch {
	case up != nil && !up.page:
		api.WriteFailure(w, api.Fail(http.StatusRequestEntityTooLarge, "too_large", up.message))
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
// reading any body at that limit. An upload route (IsUpload) gets its own
// limit instead (MaxUpload for attachments, MaxIconUpload for icons), and
// its 413 carries its message. page answers a web form's 413 (the themed
// page); nil keeps a small plain one.
func BodyLimit(limit int64, page http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := limit
			var up *upload
			if u, ok := uploadRoute(r); ok {
				n, up = u.limit, &u
			}
			if r.ContentLength > n {
				tooLarge(w, r, up, page)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
