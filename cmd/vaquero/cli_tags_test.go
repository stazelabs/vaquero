package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLITags(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "One", "--tag", "bug", "--tag", "ui")
	mustCLI(t, "ticket", "new", "VAQ", "Two", "--tag", "ui")

	table := mustCLI(t, "tags", "vaq")
	rows := strings.Split(strings.TrimSpace(table), "\n")
	if len(rows) != 3 || !regexp.MustCompile(`^NAME\s+TICKETS$`).MatchString(rows[0]) ||
		!regexp.MustCompile(`^bug\s+1$`).MatchString(rows[1]) || !regexp.MustCompile(`^ui\s+2$`).MatchString(rows[2]) {
		t.Fatalf("%q", table)
	}
	var tags []api.Tag
	if err := json.Unmarshal([]byte(mustCLI(t, "tags", "VAQ", "--json")), &tags); err != nil || len(tags) != 2 {
		t.Fatalf("%v %+v", err, tags)
	}

	if out := mustCLI(t, "tag", "edit", "VAQ", "bug", "--name", "defect"); strings.TrimSpace(out) != "defect (1 ticket)" {
		t.Fatalf("%q", out)
	}
	// Onto a tag the project has: a conflict (exit 3) until --merge.
	code, out, errOut := cli(t, "tag", "edit", "VAQ", "defect", "--name", "UI")
	if code != 3 || !strings.Contains(errOut, "VAQ already has the tag ui.") || !strings.Contains(errOut, "Add --merge to merge the two.") || out != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if out := mustCLI(t, "tag", "edit", "VAQ", "defect", "--name", "UI", "--merge"); strings.TrimSpace(out) != "ui (2 tickets)" {
		t.Fatalf("%q", out)
	}
	// The tag's own spelling, whatever was typed.
	if out := mustCLI(t, "tag", "delete", "vaq", "UI"); strings.TrimSpace(out) != "Deleted tag ui from VAQ." {
		t.Fatalf("%q", out)
	}

	if code, _, errOut := cli(t, "tag", "delete", "VAQ", "ui"); code != 1 || !strings.Contains(errOut, "No tag ui in VAQ.") {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, args := range [][]string{
		{"tag", "edit", "VAQ", "ui"},
		{"tag", "delete", "VAQ", "ui", "extra"},
		{"tag", "delete", "VAQ", "ui", "--merge"},
		{"tag", "retag", "VAQ", "ui"},
		{"tags"},
	} {
		if code, _, _ := cli(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
