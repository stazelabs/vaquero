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
	r.Get("/activity", handlers.Activity(d))
	r.Get("/events/stream", handlers.Stream(d))
	r.Get("/search", handlers.Search(d))
	r.Get("/about/licenses", handlers.Licenses(d))
	r.Get("/projects/new", handlers.ProjectNew(d))
	r.Post("/projects", handlers.ProjectCreate(d))
	r.Route("/p/{key}", func(r chi.Router) {
		r.Get("/", handlers.ProjectShow(d))
		r.Get("/board", handlers.BoardFragment(d))
		r.Get("/list", handlers.ProjectListView(d))
		r.Post("/", handlers.ProjectUpdate(d))
		r.Get("/settings", handlers.ProjectSettings(d))
		r.Post("/key", handlers.ProjectKeyChange(d))
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
		r.Get("/new", handlers.TicketNew(d))
		r.Post("/tickets", handlers.TicketCreate(d))
	})
	r.Route("/t/{ticket}", func(r chi.Router) {
		r.Get("/", handlers.TicketShow(d))
		r.Post("/", handlers.TicketUpdate(d))
		r.Get("/edit", handlers.TicketEdit(d))
		r.Post("/move", handlers.TicketMove(d))
		r.Post("/comments", handlers.CommentCreate(d))
		r.Post("/comments/{id}", handlers.CommentUpdate(d))
		r.Post("/comments/{id}/delete", handlers.CommentDelete(d))
		r.Post("/links", handlers.LinkCreate(d))
		r.Post("/links/{id}/delete", handlers.LinkDelete(d))
	})
	return r
}
