package handlers_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// tagWorld is tagged's project (VAQ-1 bug and ui, VAQ-2 ui) with a token.
func tagWorld(t *testing.T) (*webtest.Env, *webtest.Client, string) {
	t.Helper()
	e := webtest.New(t)
	tok := e.Token(t)
	tagged(t, e)
	return e, e.Client(), tok
}

func apiTags(t *testing.T, c *webtest.Client, tok string) []api.Tag {
	t.Helper()
	var tags []api.Tag
	r := c.API("GET", "/api/v1/projects/vaq/tags", tok, nil)
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	decode(t, r.Body, &tags)
	return tags
}

func TestAPITagsList(t *testing.T) {
	_, c, tok := tagWorld(t)
	tags := apiTags(t, c, tok)
	if len(tags) != 2 || tags[0] != (api.Tag{Name: "bug", TicketCount: 1}) || tags[1] != (api.Tag{Name: "ui", TicketCount: 2}) {
		t.Fatalf("%+v", tags)
	}
	if r := c.API("GET", "/api/v1/projects/NOPE/tags", tok, nil); r.Status != 404 {
		t.Fatalf("unknown project: %d", r.Status)
	}
}

func TestAPITagRename(t *testing.T) {
	e, c, tok := tagWorld(t)
	seq := seqNow(t, e)
	// The name in the path matches in any case.
	r := c.API("PATCH", "/api/v1/projects/VAQ/tags/BUG", tok, map[string]any{"name": "defect"})
	var g api.Tag
	decode(t, r.Body, &g)
	if r.Status != 200 || g != (api.Tag{Name: "defect", TicketCount: 1}) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "tag.renamed", "Owner")
	// The same name again changes nothing.
	if r := c.API("PATCH", "/api/v1/projects/VAQ/tags/defect", tok, map[string]any{"name": "defect"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)

	notFound(t, c.API("PATCH", "/api/v1/projects/VAQ/tags/bug", tok, map[string]any{"name": "x"}), "No tag bug in VAQ.")
	refused(t, c.API("PATCH", "/api/v1/projects/VAQ/tags/defect", tok, map[string]any{"name": " "}), "name", "Enter a name.")
	refused(t, c.API("PATCH", "/api/v1/projects/VAQ/tags/defect", tok, map[string]any{"name": "a,b"}), "name", "Leave commas out of tag names.")
	if r := c.API("PATCH", "/api/v1/projects/VAQ/tags/defect", tok, map[string]any{"name": "x", "colour": "red"}); r.Status != 400 {
		t.Fatalf("an unknown field: %d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)
}

// Renaming onto a tag the project has is a merge, refused with a 409
// naming that tag unless the request says "merge": true.
func TestAPITagMerge(t *testing.T) {
	e, c, tok := tagWorld(t)
	seq := seqNow(t, e)
	r := c.API("PATCH", "/api/v1/projects/VAQ/tags/bug", tok, map[string]any{"name": "UI"})
	var fail struct {
		Error struct {
			Code, Message string
			Current       api.Tag
		}
	}
	decode(t, r.Body, &fail)
	if r.Status != 409 || fail.Error.Code != "conflict" ||
		fail.Error.Message != "VAQ already has the tag ui. Merge the two, or choose another name." ||
		fail.Error.Current != (api.Tag{Name: "ui", TicketCount: 2}) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)
	if r := c.API("PATCH", "/api/v1/projects/VAQ/tags/bug", tok, map[string]any{"name": "UI", "merge": false}); r.Status != 409 {
		t.Fatalf("merge false: %d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)

	r = c.API("PATCH", "/api/v1/projects/VAQ/tags/bug", tok, map[string]any{"name": "UI", "merge": true})
	var g api.Tag
	decode(t, r.Body, &g)
	if r.Status != 200 || g != (api.Tag{Name: "ui", TicketCount: 2}) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	oneEvent(t, e, seq, "tag.merged", "Owner")
	if tags := apiTags(t, c, tok); len(tags) != 1 {
		t.Fatalf("%+v", tags)
	}
}

func TestAPITagDelete(t *testing.T) {
	e, c, tok := tagWorld(t)
	seq := seqNow(t, e)
	notFound(t, c.API("DELETE", "/api/v1/projects/VAQ/tags/nope", tok, nil), "No tag nope in VAQ.")
	noEvent(t, e, seq)
	// A tag only another project's tickets carry isn't this project's.
	oth, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	ticket(t, e, oth, core.TicketInput{Title: "T", Tags: []string{"ops"}})
	seq = seqNow(t, e)
	notFound(t, c.API("DELETE", "/api/v1/projects/VAQ/tags/ops", tok, nil), "No tag ops in VAQ.")
	noEvent(t, e, seq)
	if r := c.API("DELETE", "/api/v1/projects/VAQ/tags/Ui", tok, nil); r.Status != 204 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	oneEvent(t, e, seq, "tag.deleted", "Owner")
	if tags := apiTags(t, c, tok); len(tags) != 1 || tags[0].Name != "bug" {
		t.Fatalf("%+v", tags)
	}
}
