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
	// The API: bearer tokens, JSON answers and errors, no cookies.
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.APIHeaders, middleware.APIToken(middleware.APIAuth{R: d.R, Touch: d.Touch, Now: d.Now, Logger: d.Logger}))
		r.NotFound(handlers.APINotFound)
		r.MethodNotAllowed(handlers.APIMethodNotAllowed)
		r.Get("/me", handlers.APIMe(d))
		r.Get("/projects", handlers.APIProjects(d))
		r.Get("/projects/{key}", handlers.APIProject(d))
		r.Get("/projects/{key}/statuses", handlers.APIStatuses(d))
		r.Get("/projects/{key}/milestones", handlers.APIMilestones(d))
		r.Get("/projects/{key}/tickets", handlers.APITickets(d))
		r.Get("/tickets/{ticket}", handlers.APITicket(d))
		r.Get("/search", handlers.APISearch(d))
		r.Get("/events", handlers.APIEvents(d))
		r.Post("/projects", handlers.APIProjectCreate(d))
		r.Patch("/projects/{key}", handlers.APIProjectUpdate(d))
		r.Post("/projects/{key}/key", handlers.APIProjectKey(d))
		r.Post("/projects/{key}/tickets", handlers.APITicketCreate(d))
		r.Patch("/tickets/{ticket}", handlers.APITicketPatch(d))
		r.Post("/tickets/{ticket}/move", handlers.APITicketMove(d))
		r.Post("/tickets/{ticket}/comments", handlers.APICommentCreate(d))
		r.Patch("/comments/{id}", handlers.APICommentUpdate(d))
		r.Delete("/comments/{id}", handlers.APICommentDelete(d))
		r.Post("/tickets/{ticket}/links", handlers.APILinkCreate(d))
		r.Delete("/links/{id}", handlers.APILinkDelete(d))
	})

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
		r.Post("/delete", handlers.ProjectDelete(d))
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
