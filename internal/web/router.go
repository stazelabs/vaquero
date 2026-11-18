// Package web wires the HTTP router. Every route mounts in this file.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/stazelabs/vaquero/internal/web/handlers"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// NewRouter builds the full route table.
func NewRouter(d handlers.Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Recover(d.Logger),
		middleware.HostCheck(d.Cfg.AllowedHosts()),
		middleware.Logger(d.Logger),
		middleware.SecurityHeaders(false),
		middleware.CSP,
		middleware.CrossOrigin(d.Logger),
		middleware.BodyLimit(middleware.MaxBody),
		// A HEAD for a GET route is served by the GET handler (net/http drops
		// the body), so curl -I and uptime probes see 200, not a redirect.
		chimw.GetHead,
	)
	r.NotFound(handlers.NotFound(d))
	r.MethodNotAllowed(handlers.MethodNotAllowed(d))
	r.Get("/healthz", handlers.Healthz(d))
	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler()))
	r.Get("/", handlers.ProjectList(d))
	r.Get("/projects/new", handlers.ProjectNew(d))
	r.Post("/projects", handlers.ProjectCreate(d))
	r.Route("/p/{key}", func(r chi.Router) {
		r.Get("/", handlers.ProjectShow(d))
		r.Post("/", handlers.ProjectUpdate(d))
		r.Get("/settings", handlers.ProjectSettings(d))
		r.Post("/archive", handlers.ProjectArchive(d))
		r.Post("/restore", handlers.ProjectRestore(d))
		r.Post("/statuses", handlers.StatusCreate(d))
		r.Post("/statuses/{id}/rename", handlers.StatusRename(d))
		r.Post("/statuses/{id}/up", handlers.StatusUp(d))
		r.Post("/statuses/{id}/down", handlers.StatusDown(d))
		r.Post("/statuses/{id}/delete", handlers.StatusDelete(d))
		r.Post("/milestones", handlers.MilestoneCreate(d))
		r.Post("/milestones/{id}", handlers.MilestoneUpdate(d))
		r.Post("/milestones/{id}/close", handlers.MilestoneClose(d))
		r.Post("/milestones/{id}/reopen", handlers.MilestoneReopen(d))
	})
	return r
}
