package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// projectFromURL loads the {key} project. A GET with a non-canonical key
// (lowercase) is redirected 301 to the canonical path; an unknown key goes
// home with a toast. ok is false when the response is already written.
func projectFromURL(w http.ResponseWriter, r *http.Request, d Deps) (p gen.Project, ok bool) {
	key := chi.URLParam(r, "key")
	upper := strings.ToUpper(key)
	if key != upper && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		http.Redirect(w, r, strings.Replace(r.URL.Path, "/p/"+key, "/p/"+upper, 1), http.StatusMovedPermanently)
		return p, false
	}
	p, err := d.R.GetProjectByKey(r.Context(), upper)
	if errors.Is(err, sql.ErrNoRows) {
		redirectWithFlash(w, r, flash.Error, "No project "+upper+".", "/")
		return p, false
	}
	if err != nil {
		serverError(w, r, d, err)
		return p, false
	}
	return p, true
}

func projectPath(p gen.Project, rest string) string { return "/p/" + p.Key + rest }

// ProjectList is the front page.
func ProjectList(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListProjects(r.Context())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.ProjectListPage(rows))
	}
}

// ProjectNew is the new-project form.
func ProjectNew(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, d, http.StatusOK, templates.ProjectNewPage(templates.ProjectForm{}))
	}
}

// ProjectCreate makes a project. The key is uppercased for the person.
func ProjectCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := templates.ProjectForm{
			Key:         strings.ToUpper(strings.TrimSpace(r.PostFormValue("key"))),
			Name:        r.PostFormValue("name"),
			Description: r.PostFormValue("description"),
		}
		p, err := d.Core.CreateProject(r.Context(), d.Actor.ID, core.ProjectInput{Key: f.Key, Name: f.Name, Description: f.Description})
		if err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				f.Errors = fields
				render(w, r, d, http.StatusUnprocessableEntity, templates.ProjectNewPage(f))
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Project created.", projectPath(p, ""))
	}
}

// settingsPage renders the settings page with form f and status.
func settingsPage(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, f templates.ProjectForm, status int) {
	ctx := r.Context()
	statuses, err := d.R.ListStatuses(ctx, p.ID)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	milestones, err := d.R.ListMilestones(ctx, p.ID)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	v := templates.SettingsView{Project: p, Form: f, Milestones: milestones}
	for i, s := range statuses {
		v.Statuses = append(v.Statuses, templates.StatusRow{Status: s, First: i == 0, Last: i == len(statuses)-1})
	}
	render(w, r, d, status, templates.ProjectSettingsPage(v))
}

// ProjectSettings edits a project, its statuses and milestones.
func ProjectSettings(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsPage(w, r, d, p, templates.ProjectForm{Key: p.Key, Name: p.Name, Description: p.Description}, http.StatusOK)
	}
}

// ProjectUpdate saves the project's name and description.
func ProjectUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := templates.ProjectForm{Key: p.Key, Name: r.PostFormValue("name"), Description: r.PostFormValue("description")}
		if _, err := d.Core.UpdateProject(r.Context(), d.Actor.ID, p.ID, f.Name, f.Description); err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				f.Errors = fields
				settingsPage(w, r, d, p, f, http.StatusUnprocessableEntity)
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Project saved.", projectPath(p, "/settings"))
	}
}

// settingsAction runs one settings-page change and returns to the settings
// page with a toast: okMsg on success, the refusal's message otherwise.
func settingsAction(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, okMsg string, fn func() error) {
	to := projectPath(p, "/settings")
	err := fn()
	var inv *core.Invalid
	switch {
	case err == nil:
		redirectWithFlash(w, r, flash.Success, okMsg, to)
	case errors.As(err, &inv):
		redirectWithFlash(w, r, flash.Error, inv.Error(), to)
	case errors.Is(err, core.ErrNotFound):
		redirectWithFlash(w, r, flash.Error, "That no longer exists.", to)
	default:
		serverError(w, r, d, err)
	}
}

// ProjectKeyChange gives the project a new key and goes to its settings
// under that key; a refusal returns to settings with the message.
func ProjectKeyChange(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		np, err := d.Core.ChangeProjectKey(r.Context(), d.Actor.ID, p.ID, r.PostFormValue("key"))
		if err != nil {
			settingsAction(w, r, d, p, "", func() error { return err })
			return
		}
		redirectWithFlash(w, r, flash.Success, "Key changed to "+np.Key+".", projectPath(np, "/settings"))
	}
}

// backupKeep is how many pre-delete snapshots are kept.
const backupKeep = 7

// ProjectDelete deletes the project and everything in it, once the person
// has typed its key. A snapshot of the database is saved first; if that
// fails, nothing is deleted.
func ProjectDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settings := projectPath(p, "/settings")
		confirm := r.PostFormValue("confirm")
		if !strings.EqualFold(strings.TrimSpace(confirm), p.Key) {
			redirectWithFlash(w, r, flash.Error, "Type "+p.Key+" to confirm.", settings)
			return
		}
		if _, err := appdb.Snapshot(r.Context(), d.RDB, d.BackupDir, d.Now()); err != nil {
			d.Logger.ErrorContext(r.Context(), "snapshot before delete", "err", err)
			redirectWithFlash(w, r, flash.Error, "Couldn't save a backup, so nothing was deleted.", settings)
			return
		}
		if _, err := appdb.PruneBackups(d.BackupDir, backupKeep); err != nil {
			d.Logger.WarnContext(r.Context(), "prune backups", "err", err)
		}
		if err := d.Core.DeleteProject(r.Context(), d.Actor.ID, p.ID, confirm); err != nil {
			settingsAction(w, r, d, p, "", func() error { return err })
			return
		}
		redirectWithFlash(w, r, flash.Success, "Project "+p.Key+" deleted. A backup was saved first.", "/")
	}
}

// ProjectArchive and ProjectRestore set the project's archived state.
func ProjectArchive(d Deps) http.HandlerFunc { return projectArchived(d, true, "Project archived.") }
func ProjectRestore(d Deps) http.HandlerFunc { return projectArchived(d, false, "Project restored.") }

func projectArchived(d Deps, archived bool, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, msg, func() error {
			return d.Core.SetProjectArchived(r.Context(), d.Actor.ID, p.ID, archived)
		})
	}
}

// ownedStatus is the {id} status when it belongs to p, else ErrNotFound.
func ownedStatus(r *http.Request, d Deps, p gen.Project) (gen.Status, error) {
	s, err := d.R.GetStatus(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) || err == nil && s.ProjectID != p.ID {
		return s, core.ErrNotFound
	}
	return s, err
}

// statusAction is a settings action on the {id} status of the {key} project.
func statusAction(d Deps, okMsg string, fn func(r *http.Request, s gen.Status) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, okMsg, func() error {
			s, err := ownedStatus(r, d, p)
			if err != nil {
				return err
			}
			return fn(r, s)
		})
	}
}

// StatusCreate adds a status.
func StatusCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, "Status added.", func() error {
			_, err := d.Core.CreateStatus(r.Context(), d.Actor.ID, p.ID, r.PostFormValue("name"), r.PostFormValue("category"))
			return err
		})
	}
}

// StatusRename, StatusUp, StatusDown and StatusDelete change one status.
func StatusRename(d Deps) http.HandlerFunc {
	return statusAction(d, "Status renamed.", func(r *http.Request, s gen.Status) error {
		return d.Core.RenameStatus(r.Context(), d.Actor.ID, s.ID, r.PostFormValue("name"))
	})
}

func StatusUp(d Deps) http.HandlerFunc {
	return statusAction(d, "Status moved.", func(r *http.Request, s gen.Status) error {
		return d.Core.MoveStatus(r.Context(), d.Actor.ID, s.ID, -1)
	})
}

func StatusDown(d Deps) http.HandlerFunc {
	return statusAction(d, "Status moved.", func(r *http.Request, s gen.Status) error {
		return d.Core.MoveStatus(r.Context(), d.Actor.ID, s.ID, +1)
	})
}

func StatusDelete(d Deps) http.HandlerFunc {
	return statusAction(d, "Status deleted.", func(r *http.Request, s gen.Status) error {
		return d.Core.DeleteStatus(r.Context(), d.Actor.ID, s.ID)
	})
}

// milestoneAction is a settings action on the {id} milestone of the {key} project.
func milestoneAction(d Deps, okMsg string, fn func(r *http.Request, m gen.Milestone) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, okMsg, func() error {
			m, err := d.R.GetMilestone(r.Context(), chi.URLParam(r, "id"))
			if errors.Is(err, sql.ErrNoRows) || err == nil && m.ProjectID != p.ID {
				return core.ErrNotFound
			}
			if err != nil {
				return err
			}
			return fn(r, m)
		})
	}
}

func milestoneInput(r *http.Request) core.MilestoneInput {
	return core.MilestoneInput{Name: r.PostFormValue("name"), Description: r.PostFormValue("description"), DueOn: r.PostFormValue("due_on")}
}

// MilestoneCreate adds a milestone.
func MilestoneCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, "Milestone added.", func() error {
			_, err := d.Core.CreateMilestone(r.Context(), d.Actor.ID, p.ID, milestoneInput(r))
			return err
		})
	}
}

// MilestoneUpdate, MilestoneClose and MilestoneReopen change one milestone.
func MilestoneUpdate(d Deps) http.HandlerFunc {
	return milestoneAction(d, "Milestone saved.", func(r *http.Request, m gen.Milestone) error {
		in := milestoneInput(r)
		in.Description = m.Description // the settings form doesn't edit it
		return d.Core.UpdateMilestone(r.Context(), d.Actor.ID, m.ID, in)
	})
}

func MilestoneClose(d Deps) http.HandlerFunc {
	return milestoneAction(d, "Milestone closed.", func(r *http.Request, m gen.Milestone) error {
		return d.Core.SetMilestoneClosed(r.Context(), d.Actor.ID, m.ID, true)
	})
}

func MilestoneReopen(d Deps) http.HandlerFunc {
	return milestoneAction(d, "Milestone reopened.", func(r *http.Request, m gen.Milestone) error {
		return d.Core.SetMilestoneClosed(r.Context(), d.Actor.ID, m.ID, false)
	})
}
