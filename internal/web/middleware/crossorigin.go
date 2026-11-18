package middleware

import (
	"log/slog"
	"net/http"
)

// CrossOrigin rejects cross-site unsafe requests using the standard
// library's Sec-Fetch-Site / Origin check (Go 1.25+), in place of CSRF
// tokens: forms need no hidden field, so no form can forget one. Requests
// with neither header (non-browser clients) are allowed; they carry no
// ambient browser cookies an attacker could ride. There are no bypasses.
func CrossOrigin(logger *slog.Logger) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.WarnContext(r.Context(), "cross-origin request blocked", "path", redactPath(r.URL.Path),
			"origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"))
		http.Error(w, "This request was blocked because it came from another site.", http.StatusForbidden)
	}))
	return cop.Handler
}
