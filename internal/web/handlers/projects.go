package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// projectFromURL loads the {key} project. A GET with a non-canonical key
// (lowercase) is redirected 301 to the canonical path, query kept; an unknown key goes
// home with a toast. ok is false when the response is already written.
func projectFromURL(w http.ResponseWriter, r *http.Request, d Deps) (p gen.Project, ok bool) {
	key := chi.URLParam(r, "key")
	upper := strings.ToUpper(key)
	if key != upper && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		to := strings.Replace(r.URL.Path, "/p/"+key, "/p/"+upper, 1)
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, to, http.StatusMovedPermanently)
		return p, false
	}
	p, err := d.R.GetProjectByKey(r.Context(), upper)
	if errors.Is(err, sql.ErrNoRows) {
		// A live refresh of a page whose project was deleted or re-keyed
		// reloads the whole page rather than swapping a redirect into it.
		if r.Header.Get("HX-Request") == "true" {
			refreshWith(w, "No project "+upper+".")
			return p, false
		}
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

// settingsPage renders the settings page with form f and status; set, if
// given, fills in a refused smaller form.
func settingsPage(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, f templates.ProjectForm, status int, set ...func(*templates.SettingsView)) {
	templates.LoadEventSeq(r.Context()) // before the page data is read
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
	tags, err := d.R.ListProjectTags(ctx, p.ID)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	v := templates.SettingsView{Project: p, Form: f, Milestones: milestones}
	for _, t := range tags {
		v.Tags = append(v.Tags, templates.TagRow{ID: t.ID, Name: t.Name, Tickets: t.Tickets})
	}
	for i, s := range statuses {
		v.Statuses = append(v.Statuses, templates.StatusRow{Status: s, First: i == 0, Last: i == len(statuses)-1})
	}
	v.TierMap = core.EffectiveTierMap(p.TierMap).Indent()
	if p.Workflow == 1 {
		v.TriageMissing = missingTriage(statuses)
	}
	for _, fn := range set {
		fn(&v)
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
		if _, err := d.Core.UpdateProject(r.Context(), d.Actor.ID, p.ID, core.ProjectPatch{Name: &f.Name, Description: &f.Description}); err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				f.Errors = fields
				settingsPage(w, r, d, p, f, http.StatusUnprocessableEntity)
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Project saved.", projectPath(p, "/settings"))
	}
}

// triageStatuses are the backlog statuses the agent workflow parks
// tickets in, in order.
var triageStatuses = []string{"Needs decision", "Needs design"}

// missingTriage is the triage statuses the project lacks. A status of the
// same name in any category counts as present, since names are unique.
func missingTriage(ss []gen.Status) []string {
	var out []string
	for _, name := range triageStatuses {
		if !slices.ContainsFunc(ss, func(s gen.Status) bool { return strings.EqualFold(s.Name, name) }) {
			out = append(out, name)
		}
	}
	return out
}

// ProjectWorkflow saves the workflow switch and the tier map. A refused map
// re-renders settings with its message and what was typed.
func ProjectWorkflow(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		on := r.PostFormValue("workflow") == "on"
		tierMap := r.PostFormValue("tier_map")
		sf := templates.SubForm{Values: map[string]string{"workflow": r.PostFormValue("workflow"), "tier_map": tierMap}}
		settingsForm(w, r, d, p, "Workflow saved.", sf, func(v *templates.SettingsView, sf templates.SubForm) { v.Workflow = sf }, func() error {
			_, err := d.Core.UpdateProject(r.Context(), d.Actor.ID, p.ID, core.ProjectPatch{Workflow: &on, TierMap: &tierMap})
			return err
		})
	}
}

// TriageStatuses adds the missing triage statuses at the end of the
// backlog category, one status (and one event) each.
func TriageStatuses(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, "Triage statuses added.", func() error {
			ss, err := d.R.ListStatuses(r.Context(), p.ID)
			if err != nil {
				return err
			}
			for _, name := range missingTriage(ss) {
				if _, err := d.Core.CreateStatusAt(r.Context(), d.Actor.ID, p.ID, name, "backlog", core.StatusPlace{}); err != nil {
					return err
				}
			}
			return nil
		})
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

// settingsForm runs a settings-page form with typed fields. A refusal that
// names only those fields re-renders the page with 422, the messages and
// what was typed (sf, put in place by set); anything else is settingsAction's.
func settingsForm(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, okMsg string, sf templates.SubForm, set func(*templates.SettingsView, templates.SubForm), fn func() error) {
	err := fn()
	var inv *core.Invalid
	if errors.As(err, &inv) && namesOnly(inv.Fields, sf.Values) {
		sf.Errors = inv.Fields
		settingsPage(w, r, d, p, templates.ProjectForm{Key: p.Key, Name: p.Name, Description: p.Description}, http.StatusUnprocessableEntity,
			func(v *templates.SettingsView) { set(v, sf) })
		return
	}
	settingsAction(w, r, d, p, okMsg, func() error { return err })
}

// namesOnly reports whether every field in errs is one of the form's.
func namesOnly(errs, values map[string]string) bool {
	for k := range errs {
		if _, ok := values[k]; !ok {
			return false
		}
	}
	return true
}

// formValues is the named fields of the posted form, as typed.
func formValues(r *http.Request, names ...string) map[string]string {
	v := make(map[string]string, len(names))
	for _, n := range names {
		v[n] = r.PostFormValue(n)
	}
	return v
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

// snapshot saves a backup before a delete. Backups are named by the second,
// so a second delete in the same second takes the next free name.
func snapshot(r *http.Request, d Deps) error {
	_, err := appdb.SnapshotNext(r.Context(), d.RDB, d.BackupDir, appdb.KindPreDelete, d.Now())
	return err
}

// backupFailMessage tells the person why a delete didn't happen.
func backupFailMessage(err error, dir string) string {
	if errors.Is(err, appdb.ErrLowSpace) {
		return "Not enough free disk space to save a backup, so nothing was deleted."
	}
	return "Couldn't save a backup in " + dir + ", so nothing was deleted."
}

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
		if err := snapshot(r, d); err != nil {
			d.Logger.ErrorContext(r.Context(), "snapshot before delete", "err", err)
			redirectWithFlash(w, r, flash.Error, backupFailMessage(err, d.BackupDir), settings)
			return
		}
		if _, err := appdb.PruneBackups(d.BackupDir, appdb.BackupKeep); err != nil {
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
		sf := templates.SubForm{Values: formValues(r, "name", "category")}
		settingsForm(w, r, d, p, "Status added.", sf, func(v *templates.SettingsView, f templates.SubForm) { v.NewStatus = f }, func() error {
			_, err := d.Core.CreateStatus(r.Context(), d.Actor.ID, p.ID, sf.Values["name"], sf.Values["category"])
			return err
		})
	}
}

// StatusRename, StatusUp, StatusDown and StatusDelete change one status.
func StatusRename(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		sf := templates.SubForm{ID: chi.URLParam(r, "id"), Values: formValues(r, "name")}
		settingsForm(w, r, d, p, "Status renamed.", sf, func(v *templates.SettingsView, f templates.SubForm) { v.EditStatus = f }, func() error {
			s, err := ownedStatus(r, d, p)
			if err != nil {
				return err
			}
			return d.Core.RenameStatus(r.Context(), d.Actor.ID, s.ID, sf.Values["name"])
		})
	}
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
			m, err := ownedMilestone(r, d, p)
			if err != nil {
				return err
			}
			return fn(r, m)
		})
	}
}

// ownedMilestone is the {id} milestone when it belongs to p, else ErrNotFound.
func ownedMilestone(r *http.Request, d Deps, p gen.Project) (gen.Milestone, error) {
	m, err := d.R.GetMilestone(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) || err == nil && m.ProjectID != p.ID {
		return m, core.ErrNotFound
	}
	return m, err
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
		sf := templates.SubForm{Values: formValues(r, "name", "due_on")}
		settingsForm(w, r, d, p, "Milestone added.", sf, func(v *templates.SettingsView, f templates.SubForm) { v.NewMilestone = f }, func() error {
			_, err := d.Core.CreateMilestone(r.Context(), d.Actor.ID, p.ID, milestoneInput(r))
			return err
		})
	}
}

// MilestoneUpdate, MilestoneClose and MilestoneReopen change one milestone.
func MilestoneUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		sf := templates.SubForm{ID: chi.URLParam(r, "id"), Values: formValues(r, "name", "due_on", "description")}
		settingsForm(w, r, d, p, "Milestone saved.", sf, func(v *templates.SettingsView, f templates.SubForm) { v.EditMilestone = f }, func() error {
			m, err := ownedMilestone(r, d, p)
			if err != nil {
				return err
			}
			in := milestoneInput(r)
			if _, sent := r.PostForm["description"]; !sent {
				in.Description = m.Description // a form from before the field keeps it
			}
			return d.Core.UpdateMilestone(r.Context(), d.Actor.ID, m.ID, in)
		})
	}
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

// mergeTarget is the project's other tag that name would merge tag id
// into (matched by core.SameTag), if any.
func mergeTarget(r *http.Request, d Deps, p gen.Project, id, name string) (gen.ListProjectTagsRow, bool, error) {
	rows, err := d.R.ListProjectTags(r.Context(), p.ID)
	if err != nil {
		return gen.ListProjectTagsRow{}, false, err
	}
	name = strings.TrimSpace(name)
	for _, t := range rows {
		if t.ID != id && core.SameTag(t.Name, name) {
			return t, true, nil
		}
	}
	return gen.ListProjectTagsRow{}, false, nil
}

// TagRename renames one of the project's tags. A name another of its tags
// has merges the two, but only when the form says merge=1: otherwise the
// page comes back with the question and a merge button (behind the confirm
// dialog).
func TagRename(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		sf := templates.SubForm{ID: id, Values: formValues(r, "name")}
		into, merging, err := mergeTarget(r, d, p, id, sf.Values["name"])
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if merging && r.PostFormValue("merge") != "1" {
			sf.Errors = map[string]string{"name": p.Key + " already has the tag " + into.Name + ". Merge the two, or choose another name."}
			settingsPage(w, r, d, p, templates.ProjectForm{Key: p.Key, Name: p.Name, Description: p.Description}, http.StatusUnprocessableEntity,
				func(v *templates.SettingsView) { v.EditTag, v.MergeInto = sf, into.Name })
			return
		}
		msg := "Tag renamed."
		if merging {
			msg = "Tags merged."
		}
		settingsForm(w, r, d, p, msg, sf, func(v *templates.SettingsView, f templates.SubForm) { v.EditTag = f }, func() error {
			return d.Core.RenameTag(r.Context(), d.Actor.ID, p.ID, id, sf.Values["name"])
		})
	}
}

// TagDelete takes one of the project's tags off all its tickets.
func TagDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, "Tag deleted.", func() error {
			return d.Core.DeleteTag(r.Context(), d.Actor.ID, p.ID, chi.URLParam(r, "id"))
		})
	}
}
