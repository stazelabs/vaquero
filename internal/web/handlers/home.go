package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/web/templates"
)

// Home is the front page.
func Home(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, d, http.StatusOK, templates.HomePage())
	}
}
