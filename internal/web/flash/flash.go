// Package flash carries one toast message across a redirect.
//
// Local mode has no sign-in sessions, so the message rides in a short-lived
// cookie. It holds only text the server wrote, shown back to the same
// browser through an escaping template, so it needs no signature.
package flash

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// Kind picks the toast's colour.
type Kind string

const (
	Success Kind = "success"
	Error   Kind = "error"
)

// Flash is one toast.
type Flash struct {
	Kind    Kind
	Message string
}

const cookieName = "vq_flash"

// Put queues a toast for the next page this browser renders.
func Put(w http.ResponseWriter, kind Kind, msg string) {
	b, _ := json.Marshal(Flash{Kind: kind, Message: msg})
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: base64.RawURLEncoding.EncodeToString(b),
		Path: "/", MaxAge: 60, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Pop takes the queued toast, if any, and expires the cookie. A malformed
// cookie is dropped silently.
func Pop(w http.ResponseWriter, r *http.Request) (Flash, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return Flash{}, false
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return Flash{}, false
	}
	var f Flash
	if err := json.Unmarshal(b, &f); err != nil || f.Message == "" || (f.Kind != Success && f.Kind != Error) {
		return Flash{}, false
	}
	return f, true
}

type ctxKey struct{}

// WithCurrent attaches the popped toast to the render context.
func WithCurrent(ctx context.Context, f Flash) context.Context {
	return context.WithValue(ctx, ctxKey{}, f)
}

// Current returns the toast for this render.
func Current(ctx context.Context) (Flash, bool) {
	f, ok := ctx.Value(ctxKey{}).(Flash)
	return f, ok
}
