package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

// VAQ-119: vaquero project link A depends-on|relates B, shown by project
// show, and removed by project unlink A ID (or the other project's key).
func TestCLIProjectLinks(t *testing.T) {
	e := cliEnv(t)
	mustCLI(t, "project", "create", "WEB", "Web app")
	mustCLI(t, "project", "create", "APP", "Mobile app")

	if out := mustCLI(t, "project", "link", "app", "depends-on", "web"); strings.TrimSpace(out) != "APP depends on WEB." {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "project", "link", "WEB", "relates", "VAQ"); strings.TrimSpace(out) != "WEB relates to VAQ." {
		t.Fatalf("%q", out)
	}
	var l api.ProjectLink
	if err := json.Unmarshal([]byte(mustCLI(t, "project", "link", "VAQ", "depends-on", "APP", "--json")), &l); err != nil || l.Key != "APP" || l.Kind != "depends on" {
		t.Fatalf("%v %+v", err, l)
	}

	out := mustCLI(t, "project", "show", "APP")
	for _, want := range []string{"Depends on WEB  Web app", "Used by VAQ  Vaquero  " + l.ID} {
		if !strings.Contains(out, want) {
			t.Errorf("show: %q lacks %q", out, want)
		}
	}

	if code, _, errOut := cli(t, "project", "link", "WEB", "depends-on", "VAQ"); code != 1 || !strings.Contains(errOut, "That would make WEB depend on itself: VAQ depends on WEB through APP.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "link", "WEB", "blocks", "VAQ"); code != 2 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, _ := cli(t, "project", "link", "WEB", "relates"); code != 2 {
		t.Fatalf("no target: %d", code)
	}

	// By id from either end, or by the other project's key.
	if out := mustCLI(t, "project", "unlink", "APP", l.ID); strings.TrimSpace(out) != "Removed 1 link between APP and VAQ." {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "project", "unlink", "VAQ", "web"); strings.TrimSpace(out) != "Removed 1 link between VAQ and WEB." {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := cli(t, "project", "unlink", "VAQ", "WEB"); code != 1 || !strings.Contains(errOut, "VAQ isn't linked to WEB") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "unlink", "VAQ", "01JZZZZZZZZZZZZZZZZZZZZZZZ"); code != 1 || !strings.Contains(errOut, "No such link in VAQ.") {
		t.Fatalf("%d %q", code, errOut)
	}

	mustCLI(t, "project", "link", "VAQ", "relates", "APP", "--agent", "planner")
	evs, err := e.Deps.R.ListEventsNewest(t.Context(), 1)
	if err != nil || evs[0].Kind != "project.linked" || evs[0].ActorName != "planner" {
		t.Fatalf("%v %+v", err, evs)
	}
}
