package middleware

import (
	"context"
	"net/http"
)

type pathKey struct{}

// CurrentPath puts the request's path on the context, so the app shell can
// mark the sidebar item for the page being shown.
func CurrentPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), pathKey{}, r.URL.Path)))
	})
}

// PathFromContext is the path CurrentPath stored, or "" without it.
func PathFromContext(ctx context.Context) string {
	p, _ := ctx.Value(pathKey{}).(string)
	return p
}
