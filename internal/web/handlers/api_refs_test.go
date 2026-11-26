package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func TestAPIReferences(t *testing.T) {
	e, c, tok := apiWorld(t)
	// Left out of a ticket with none.
	if r := c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil); strings.Contains(r.Body, "references") {
		t.Fatalf("empty references shown: %s", r.Body)
	}
	r := c.API("POST", "/api/v1/tickets/vaq-1/refs", tok, map[string]string{"kind": "design", "target": "https://example.com/d", "label": "Mock", "anchor": "top"})
	var ref api.Ref
	decode(t, r.Body, &ref)
	if r.Status != 201 || ref.ID == "" || ref.Kind != "design" || ref.Target != "https://example.com/d" || ref.Anchor != "top" || ref.Label != "Mock" {
		t.Fatalf("add: %d %s", r.Status, r.Body)
	}
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.References) != 1 || tk.References[0] != ref {
		t.Fatalf("references %+v", tk.References)
	}

	// Refusals: 422 with the field's message.
	for body, want := range map[string]string{
		`{"kind":"spec","target":"javascript:alert(1)"}`:                  "Use a repo path or an http or https URL.",
		`{"kind":"spec","target":"docs/a.md"}`:                            "Set the project's repository first.",
		`{"kind":"spec","target":"https://example.com/d","anchor":"top"}`: "VAQ-1 already references https://example.com/d.",
		`{"kind":"memo","target":"https://example.com/e"}`:                "Choose requirement, design, plan, spec or other.",
	} {
		r := c.API("POST", "/api/v1/tickets/VAQ-1/refs", tok, json.RawMessage(body))
		if r.Status != 422 || !strings.Contains(r.Body, want) {
			t.Errorf("%s: %d %s", body, r.Status, r.Body)
		}
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-99/refs", tok, map[string]string{"kind": "spec", "target": "https://x.example/"}); r.Status != 404 {
		t.Errorf("no ticket: %d", r.Status)
	}

	// A repo path, once the project has a repository.
	repo := gittest.Repo(t)
	repo.WriteCommit("a.md", "a", "first")
	path, main := repo.Dir, "main"
	p, err := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{RepoPath: &path, MainBranch: &main}); err != nil {
		t.Fatal(err)
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/refs", tok, map[string]string{"kind": "spec", "target": "docs/not-on-main.md"}); r.Status != 201 {
		t.Fatalf("repo path: %d %s", r.Status, r.Body)
	}

	// Another ticket's reference isn't this one's: 404.
	if r := c.API("DELETE", "/api/v1/tickets/VAQ-2/refs/"+ref.ID, tok, nil); r.Status != 404 {
		t.Fatalf("wrong ticket: %d %s", r.Status, r.Body)
	}
	if r := c.API("DELETE", "/api/v1/tickets/VAQ-1/refs/"+ref.ID, tok, nil); r.Status != 204 {
		t.Fatalf("remove: %d %s", r.Status, r.Body)
	}
	if r := c.API("DELETE", "/api/v1/tickets/VAQ-1/refs/"+ref.ID, tok, nil); r.Status != 404 {
		t.Fatalf("again: %d %s", r.Status, r.Body)
	}
}
