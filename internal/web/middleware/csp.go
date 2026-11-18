package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/a-h/templ"
)

// CSP sets a nonce-based Content-Security-Policy and stores the nonce where
// templ components read it (templ.GetNonce).
func CSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		nonce := base64.StdEncoding.EncodeToString(b)
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self'; img-src 'self' data:; "+
				"font-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		next.ServeHTTP(w, r.WithContext(templ.WithNonce(r.Context(), nonce)))
	})
}
