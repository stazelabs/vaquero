package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// The tag API (VAQ-134): a project's tags, named in the path in any case
// (by core.SameTag), renamed, merged and deleted through the same core
// calls as the settings page.

func tagDTO(t gen.ListProjectTagsRow) api.Tag { return api.Tag{Name: t.Name, TicketCount: t.Tickets} }

// apiTag loads the {key} project, its tags and its {name} tag; a name no
// tag of the project matches is a 404, as for statuses.
func apiTag(w http.ResponseWriter, r *http.Request, d Deps) (gen.Project, gen.ListProjectTagsRow, bool) {
	p, ok := apiProject(w, r, d)
	if !ok {
		return p, gen.ListProjectTagsRow{}, false
	}
	tags, err := d.R.ListProjectTags(r.Context(), p.ID)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return p, gen.ListProjectTagsRow{}, false
	}
	name := pathName(r)
	for _, t := range tags {
		if core.SameTag(t.Name, strings.TrimSpace(name)) {
			return p, t, true
		}
	}
	api.WriteFailure(w, api.NotFound("No tag "+name+" in "+p.Key+"."))
	return p, gen.ListProjectTagsRow{}, false
}

// writeTag answers the project's tag called name (after a rename or a
// merge, the tag its tickets now carry).
func writeTag(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, name string) {
	tags, err := d.R.ListProjectTags(r.Context(), p.ID)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	for _, t := range tags {
		if core.SameTag(t.Name, name) {
			api.WriteJSON(w, http.StatusOK, tagDTO(t))
			return
		}
	}
	apiFail(w, r, d, api.Internal(), fmt.Errorf("tag %q missing from its project's list", name))
}

// APITags is GET /api/v1/projects/{key}/tags.
func APITags(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		tags, err := d.R.ListProjectTags(r.Context(), p.ID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Tag, len(tags))
		for i, t := range tags {
			out[i] = tagDTO(t)
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APITagUpdate is PATCH /api/v1/projects/{key}/tags/{name} {name, merge?}.
// A name another of the project's tags has is a merge, refused with a 409
// naming that tag unless merge is true.
func APITagUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, t, ok := apiTag(w, r, d)
		if !ok {
			return
		}
		var in struct {
			Name  *string
			Merge bool
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		if in.Name == nil {
			writeTag(w, r, d, p, t.Name)
			return
		}
		into, merging, err := mergeTarget(r, d, p, t.ID, *in.Name)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		if merging && !in.Merge {
			f := api.Fail(http.StatusConflict, "conflict",
				p.Key+" already has the tag "+into.Name+". Merge the two, or choose another name.")
			f.Current = tagDTO(into)
			api.WriteFailure(w, f)
			return
		}
		if err := d.Core.RenameTag(r.Context(), apiActor(r).ID, p.ID, t.ID, *in.Name); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		name := strings.TrimSpace(*in.Name)
		if merging {
			name = into.Name
		}
		writeTag(w, r, d, p, name)
	}
}

// APITagDelete is DELETE /api/v1/projects/{key}/tags/{name}: the tag comes
// off every ticket of the project; other projects keep it.
func APITagDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, t, ok := apiTag(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.DeleteTag(r.Context(), apiActor(r).ID, p.ID, t.ID); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
