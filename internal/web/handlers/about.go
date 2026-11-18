package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero"
	"github.com/stazelabs/vaquero/internal/legal"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// Licenses is GET /about/licenses: vaquero's licence and the open source
// notices for everything it bundles.
func Licenses(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, d, http.StatusOK, templates.LicensesPage(vaquero.License(), vaquero.Notice(), legal.ThirdParty()))
	}
}
