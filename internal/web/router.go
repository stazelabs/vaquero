// Package web wires the HTTP router. Every route mounts in this file.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/mcpserver"

	"github.com/stazelabs/vaquero/internal/web/handlers"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// NewRouter builds the full route table.
func NewRouter(d handlers.Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Recover(d.Logger),
		middleware.HostCheck(d.Cfg.AllowedHosts(), d.Logger),
		middleware.Logger(d.Logger),
		middleware.SecurityHeaders(false),
		middleware.CSP,
		middleware.CrossOrigin(d.Logger),
		// Names and avatars, read on a page's first use (VAQ-149); before
		// BodyLimit, so its themed 413 page shows the owner's face.
		handlers.Faces(d),
		// The sidebar's place, from the path, and its projects, read once
		// on a page's first use (VAQ-171).
		handlers.Nav(d),
		// 1 MiB, except an upload route (middleware.IsUpload): MaxUpload
		// for attachments, MaxIconUpload for project icons.
		middleware.BodyLimit(middleware.MaxBody, handlers.TooLarge(d)),
		// Provenance for every change: the surface and the Vaquero-Client,
		// -Harness, -Model and -Run headers, as reported (core.Origin).
		middleware.Origin,
		// A HEAD for a GET route is served by the GET handler (net/http drops
		// the body), so curl -I and uptime probes see 200, not a redirect.
		chimw.GetHead,
	)
	r.NotFound(handlers.NotFound(d))
	r.MethodNotAllowed(handlers.MethodNotAllowed(d))
	r.Get("/healthz", handlers.Healthz(d))
	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler()))
	// The API: bearer tokens, JSON answers and errors, no cookies. MCP is
	// the same API as tools, behind the same token check, calling apiRoutes
	// in-process as the request's actor.
	auth := middleware.APIToken(middleware.APIAuth{R: d.R, Touch: d.Touch, Now: d.Now, Logger: d.Logger,
		Local: d.Cfg.Mode == config.ModeLocal, OwnerID: d.Actor.ID, Agent: d.Core.EnsureAgent})
	apiRoutes := chi.NewRouter()
	apiRoutes.Route("/api/v1", func(r chi.Router) { mountAPI(r, d) })
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.APIHeaders, auth)
		mountAPI(r, d)
	})
	r.Route("/mcp", func(r chi.Router) {
		r.Use(middleware.APIHeaders, auth)
		r.MethodNotAllowed(handlers.APIMethodNotAllowed)
		r.Post("/", mcpserver.Handler(apiRoutes, d.Logger, d.MCPCalls, d.Now).ServeHTTP)
	})

	r.Get("/events/stream", handlers.Stream(d))
	// Attachment bytes: no project key in the URL, so references survive a
	// key change. The bare id redirects to the canonical name.
	r.Get("/thumbs/{id}", handlers.AttachmentThumb(d))
	r.Get("/attachments/{id}", handlers.AttachmentServe(d))
	r.Get("/attachments/{id}/*", handlers.AttachmentServe(d))
	r.Get("/avatars/{actor}", handlers.AvatarServe(d))
	// Pages: each records the newest event sequence before it reads
	// anything, for its live stream (handlers.PageSeq); so does a form POST
	// whose refusal re-renders its page (fs).
	fs := handlers.FormSeq(d)
	r.Group(func(r chi.Router) {
		r.Use(handlers.PageSeq(d))
		r.Route("/admin", func(r chi.Router) {
			r.Get("/", handlers.AdminHome(d))
			r.Get("/tokens", handlers.AdminTokens(d))
			r.With(fs).Post("/tokens", handlers.AdminTokenCreate(d))
			r.Post("/tokens/{id}/revoke", handlers.AdminTokenRevoke(d))
			r.Get("/mcp", handlers.AdminMCP(d))
			r.Get("/backups", handlers.AdminBackups(d))
			r.Post("/backups/snapshot", handlers.AdminBackupSnapshot(d))
			r.Post("/backups/{name}/delete", handlers.AdminBackupDelete(d))
		})

		r.Get("/", handlers.ProjectList(d))
		r.Get("/profile", handlers.Profile(d))
		r.With(fs).Post("/profile", handlers.ProfileSave(d))
		r.Post("/profile/avatar", handlers.ProfileAvatarSave(d))
		r.Post("/profile/avatar/delete", handlers.ProfileAvatarRemove(d))
		r.Post("/decisions/{id}/answer", handlers.DecisionAnswer(d))
		r.Get("/my", handlers.MyWork(d))
		r.Get("/activity", handlers.Activity(d))
		r.Get("/search", handlers.Search(d))
		r.Get("/about/licenses", handlers.Licenses(d))
		r.Get("/help", handlers.HelpIndex(d))
		r.Get("/help/{slug}", handlers.HelpArticle(d))
		r.Get("/projects/new", handlers.ProjectNew(d))
		r.With(fs).Post("/projects", handlers.ProjectCreate(d))
		r.Route("/p/{key}", func(r chi.Router) {
			// Focus is the landing page; it takes the board's filters, so an
			// old board address opens it filtered the same way.
			r.Get("/", handlers.ProjectFocus(d))
			r.Get("/focus/fragment", handlers.FocusFragment(d))
			r.Get("/board", handlers.ProjectBoard(d))
			r.Get("/board/fragment", handlers.BoardFragment(d))
			r.Get("/list", handlers.ProjectListView(d))
			r.With(fs).Post("/", handlers.ProjectUpdate(d))
			r.Get("/settings", handlers.ProjectSettings(d))
			r.Get("/icon", handlers.ProjectIconServe(d))
			r.Get("/icon/choices", handlers.ProjectIconChoices(d))
			r.Post("/icon", handlers.ProjectIconSave(d))
			r.Post("/icon/delete", handlers.ProjectIconRemove(d))
			r.Post("/key", handlers.ProjectKeyChange(d))
			r.With(fs).Post("/workflow", handlers.ProjectWorkflow(d))
			r.With(fs).Post("/repo", handlers.ProjectRepo(d))
			r.Post("/repo/fetch", handlers.ProjectFetch(d))
			r.Post("/triage-statuses", handlers.TriageStatuses(d))
			r.Post("/delete", handlers.ProjectDelete(d))
			r.With(fs).Post("/links", handlers.ProjectLinkCreate(d))
			r.Post("/links/{id}/delete", handlers.ProjectLinkDelete(d))
			r.Post("/archive", handlers.ProjectArchive(d))
			r.Post("/restore", handlers.ProjectRestore(d))
			r.With(fs).Post("/statuses", handlers.StatusCreate(d))
			r.With(fs).Post("/statuses/{id}/rename", handlers.StatusRename(d))
			r.Post("/statuses/{id}/up", handlers.StatusUp(d))
			r.Post("/statuses/{id}/down", handlers.StatusDown(d))
			r.Post("/statuses/{id}/delete", handlers.StatusDelete(d))
			r.With(fs).Post("/milestones", handlers.MilestoneCreate(d))
			r.With(fs).Post("/milestones/{id}", handlers.MilestoneUpdate(d))
			r.With(fs).Post("/tags/{id}/rename", handlers.TagRename(d))
			r.Post("/tags/{id}/delete", handlers.TagDelete(d))
			r.Post("/milestones/{id}/close", handlers.MilestoneClose(d))
			r.Post("/milestones/{id}/reopen", handlers.MilestoneReopen(d))
			r.Get("/files/*", handlers.FileView(d))
			r.Get("/file-changes/*", handlers.FileChanges(d))
			r.Get("/new", handlers.TicketNew(d))
			r.With(fs).Post("/tickets", handlers.TicketCreate(d))
		})
		r.Route("/t/{ticket}", func(r chi.Router) {
			r.Get("/", handlers.TicketShow(d))
			r.With(fs).Post("/", handlers.TicketUpdate(d))
			r.Get("/edit", handlers.TicketEdit(d))
			r.Post("/move", handlers.TicketMove(d))
			r.Post("/move-to", handlers.TicketMoveTo(d))
			r.With(fs).Post("/comments", handlers.CommentCreate(d))
			r.Post("/comments/{id}", handlers.CommentUpdate(d))
			r.Post("/comments/{id}/delete", handlers.CommentDelete(d))
			r.With(fs).Post("/links", handlers.LinkCreate(d))
			r.Post("/links/{id}/delete", handlers.LinkDelete(d))
			r.Post("/refs/{id}/delete", handlers.RefDelete(d))
			r.Get("/link-targets", handlers.LinkTargets(d))
			r.Get("/commits", handlers.TicketCommits(d))
			r.Get("/changes/{seq}", handlers.TicketChanges(d))
			r.Post("/approval/ask", handlers.ApprovalAsk(d))
			r.Post("/approval/ask-again", handlers.ApprovalAskAgain(d))
			r.Post("/attachments", handlers.AttachmentUpload(d))
			r.Post("/attachments/{id}/delete", handlers.AttachmentDelete(d))
		})
	})
	return r
}

// mountAPI adds the /api/v1 routes to r.
func mountAPI(r chi.Router, d handlers.Deps) {
	r.NotFound(handlers.APINotFound)
	r.MethodNotAllowed(handlers.APIMethodNotAllowed)
	r.Get("/me", handlers.APIMe(d))
	r.Get("/profile", handlers.APIProfile(d))
	r.Patch("/profile", handlers.APIProfileUpdate(d))
	r.Put("/profile/avatar", handlers.APIProfileAvatarUpload(d))
	r.Delete("/profile/avatar", handlers.APIProfileAvatarDelete(d))
	r.Get("/export", handlers.APIExport(d))
	r.Get("/projects", handlers.APIProjects(d))
	r.Get("/projects/{key}", handlers.APIProject(d))
	r.Get("/projects/{key}/statuses", handlers.APIStatuses(d))
	r.Get("/projects/{key}/milestones", handlers.APIMilestones(d))
	r.Get("/projects/{key}/tags", handlers.APITags(d))
	r.Get("/projects/{key}/tickets", handlers.APITickets(d))
	r.Get("/tickets/{ticket}", handlers.APITicket(d))
	r.Get("/projects/{key}/files/*", handlers.APIFile(d))
	r.Get("/projects/{key}/repo/status", handlers.APIRepoStatus(d))
	r.Post("/projects/{key}/repo/fetch", handlers.APIRepoFetch(d))
	r.Get("/tickets/{ticket}/commits", handlers.APITicketCommits(d))
	r.Get("/search", handlers.APISearch(d))
	r.Get("/events", handlers.APIEvents(d))
	r.Post("/projects", handlers.APIProjectCreate(d))
	r.Patch("/projects/{key}", handlers.APIProjectUpdate(d))
	r.Post("/projects/{key}/key", handlers.APIProjectKey(d))
	r.Post("/projects/{key}/links", handlers.APIProjectLinkCreate(d))
	r.Delete("/projects/{key}/links/{id}", handlers.APIProjectLinkDelete(d))
	r.Put("/projects/{key}/icon", handlers.APIProjectIconUpload(d))
	r.Delete("/projects/{key}/icon", handlers.APIProjectIconDelete(d))
	r.Post("/projects/{key}/statuses", handlers.APIStatusCreate(d))
	r.Patch("/projects/{key}/statuses/{name}", handlers.APIStatusUpdate(d))
	r.Post("/projects/{key}/statuses/{name}/move", handlers.APIStatusMove(d))
	r.Delete("/projects/{key}/statuses/{name}", handlers.APIStatusDelete(d))
	r.Patch("/projects/{key}/tags/{name}", handlers.APITagUpdate(d))
	r.Delete("/projects/{key}/tags/{name}", handlers.APITagDelete(d))
	r.Post("/projects/{key}/milestones", handlers.APIMilestoneCreate(d))
	r.Patch("/projects/{key}/milestones/{name}", handlers.APIMilestoneUpdate(d))
	r.Post("/projects/{key}/milestones/{name}/close", handlers.APIMilestoneClose(d))
	r.Post("/projects/{key}/milestones/{name}/reopen", handlers.APIMilestoneReopen(d))
	r.Post("/projects/{key}/tickets", handlers.APITicketCreate(d))
	r.Post("/tickets/bulk", handlers.APITicketsBulk(d))
	r.Patch("/tickets/{ticket}", handlers.APITicketPatch(d))
	r.Post("/tickets/{ticket}/move", handlers.APITicketMove(d))
	r.Post("/tickets/{ticket}/move-to", handlers.APITicketMoveTo(d))
	r.Get("/ready", handlers.APIReady(d))
	r.Post("/tickets/{ticket}/claim", handlers.APIClaim(d))
	r.Post("/tickets/{ticket}/heartbeat", handlers.APIHeartbeat(d))
	r.Post("/tickets/{ticket}/unclaim", handlers.APIUnclaim(d))
	r.Post("/tickets/{ticket}/comments", handlers.APICommentCreate(d))
	r.Patch("/comments/{id}", handlers.APICommentUpdate(d))
	r.Delete("/comments/{id}", handlers.APICommentDelete(d))
	r.Get("/tickets/{ticket}/plan", handlers.APIPlan(d))
	r.Put("/tickets/{ticket}/plan", handlers.APIPlanSave(d))
	r.Get("/tickets/{ticket}/plan/revisions/{n}", handlers.APIPlanRevision(d))
	r.Post("/decisions", handlers.APIDecisionCreate(d))
	r.Get("/decisions", handlers.APIDecisions(d))
	r.Get("/decisions/{id}", handlers.APIDecision(d))
	r.Post("/decisions/{id}/answer", handlers.APIDecisionAnswer(d))
	r.Post("/decisions/{id}/withdraw", handlers.APIDecisionWithdraw(d))
	r.Get("/tickets/{ticket}/attachments", handlers.APIAttachmentList(d))
	r.Post("/tickets/{ticket}/attachments", handlers.APIAttachmentUpload(d))
	r.Get("/attachments/{id}", handlers.APIAttachmentGet(d))
	r.Delete("/attachments/{id}", handlers.APIAttachmentDelete(d))
	r.Post("/tickets/{ticket}/links", handlers.APILinkCreate(d))
	r.Delete("/links/{id}", handlers.APILinkDelete(d))
	r.Post("/tickets/{ticket}/refs", handlers.APIRefCreate(d))
	r.Delete("/tickets/{ticket}/refs/{id}", handlers.APIRefDelete(d))
	r.Get("/tokens", handlers.APITokens(d))
	r.Post("/tokens", handlers.APITokenCreate(d))
	r.Post("/tokens/{prefix}/revoke", handlers.APITokenRevoke(d))
}
