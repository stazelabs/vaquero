package middleware

import (
	"net/http"
	"strings"

	"github.com/stazelabs/vaquero/internal/core"
)

// Provenance headers: what the caller says it is, recorded with each change
// (core.Origin) and never trusted as identity.
const (
	HeaderClient  = "Vaquero-Client"  // "vaquero-cli/0.1.6", "vaquero-mcp/0.1.6"
	HeaderHarness = "Vaquero-Harness" // the coding agent's harness and version
	HeaderModel   = "Vaquero-Model"
	HeaderRun     = "Vaquero-Run"
)

// CLIClientPrefix starts the Vaquero-Client of the vaquero command line.
const CLIClientPrefix = "vaquero-cli/"

// Origin puts the request's core.Origin on its context: the surface from
// the path (mcp for /mcp, cli for /api/v1 when Vaquero-Client names the
// vaquero command line, api for the rest of /api/v1, web for pages) and
// the provenance headers as sent. core cuts each value when it stores it.
func Origin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := core.Origin{
			Client:  strings.TrimSpace(r.Header.Get(HeaderClient)),
			Harness: strings.TrimSpace(r.Header.Get(HeaderHarness)),
			Model:   strings.TrimSpace(r.Header.Get(HeaderModel)),
			Run:     strings.TrimSpace(r.Header.Get(HeaderRun)),
		}
		switch p := r.URL.Path; {
		case p == "/mcp" || strings.HasPrefix(p, "/mcp/"):
			o.Surface = core.SurfaceMCP
		case p == "/api" || strings.HasPrefix(p, "/api/"):
			o.Surface = core.SurfaceAPI
			if strings.HasPrefix(o.Client, CLIClientPrefix) {
				o.Surface = core.SurfaceCLI
			}
		default:
			o.Surface = core.SurfaceWeb
		}
		next.ServeHTTP(w, r.WithContext(core.WithOrigin(r.Context(), o)))
	})
}
