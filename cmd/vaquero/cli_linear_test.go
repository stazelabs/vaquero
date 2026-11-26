package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

// linearSample is an issue export built from the columns Linear documents
// (linearDocs), with invented rows: numbers 1 to 12 with gaps, every
// default status plus Triage, Duplicate and a custom one, a formula-quoted
// title, a multi-line description.
const linearSample = "testdata/linear-issues.csv"

// writeCSV writes a CSV file into a temporary directory.
func writeCSV(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "issues.csv")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func showJSON(t *testing.T, key string) api.Ticket {
	t.Helper()
	var tk api.Ticket
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", key, "--json")), &tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

func projectKeys(t *testing.T) []string {
	t.Helper()
	var ps []api.Project
	if err := json.Unmarshal([]byte(mustCLI(t, "projects", "--json")), &ps); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range ps {
		keys = append(keys, p.Key)
	}
	slices.Sort(keys)
	return keys
}

func TestImportLinearKeepsKeysNumbersStatusesAndLabels(t *testing.T) {
	cliEnv(t)
	out := mustCLI(t, "import", "linear", linearSample)
	for _, want := range []string{
		"Imported 9 tickets into ENG (Engineering), a new project.",
		"New statuses in ENG: Triage (backlog), Duplicate (cancelled), QA (in progress).",
		"Comments, relations and attachments aren't in the CSV, so they weren't imported.",
		"Columns not imported: Estimate, Project ID, Project, Creator, Assignee, Cycle Number,",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Unknown columns") {
		t.Fatalf("the documented header has no unknown columns:\n%s", out)
	}

	for _, c := range []struct{ key, title, status string }{
		{"ENG-1", "Set up the build", "Done"},
		{"ENG-2", "Sort the settings page", "Todo"},
		{"ENG-3", "Retry the nightly sync", "In progress"},
		{"ENG-5", "=SUM(A1:A3) shows as text", "Backlog"},
		{"ENG-7", "Check the release notes", "In review"},
		{"ENG-8", "Look at the crash report", "Triage"},
		{"ENG-9", "Old login page", "Cancelled"},
		{"ENG-10", "Same as ENG-3", "Duplicate"},
		{"ENG-12", "Try the new parser", "QA"},
	} {
		if tk := showJSON(t, c.key); tk.Title != c.title || tk.Status != c.status {
			t.Fatalf("%s: %q %q", c.key, tk.Title, tk.Status)
		}
	}
	tk := showJSON(t, "ENG-3")
	if tk.Priority != 2 || !slices.Equal(tk.Tags, []string{"Backend", "Bug"}) || !strings.Contains(tk.Body, "- retry three times\n- then alert") {
		t.Fatalf("%+v", tk)
	}
	if p := showJSON(t, "ENG-7").Priority; p != 1 {
		t.Fatalf("urgent: %d", p)
	}
	if p := showJSON(t, "ENG-5").Priority; p != 0 {
		t.Fatalf("no priority: %d", p)
	}
	if code, _, _ := cli(t, "ticket", "show", "ENG-4"); code != 1 {
		t.Fatal("a number the file skips was made")
	}
	var ss []api.Status
	if err := json.Unmarshal([]byte(mustCLI(t, "statuses", "ENG", "--json")), &ss); err != nil {
		t.Fatal(err)
	}
	cat := map[string]string{}
	for _, s := range ss {
		cat[s.Name] = s.Category
	}
	if cat["Triage"] != "backlog" || cat["Duplicate"] != "cancelled" || cat["QA"] != "in_progress" || len(ss) != 9 {
		t.Fatalf("%v", cat)
	}
	// Numbering goes on after the highest imported number.
	if out := mustCLI(t, "ticket", "new", "ENG", "After the import"); !strings.Contains(out, "ENG-13") {
		t.Fatalf("%q", out)
	}

	// A project that has tickets is never filled: nothing is written.
	code, _, errOut := cli(t, "import", "linear", linearSample)
	if code != 1 || !strings.Contains(errOut, "ENG already has tickets") || !strings.Contains(errOut, "Nothing was imported.") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// --project fills an empty project, or makes a new one, under another key;
// the numbers are kept.
func TestImportLinearIntoAnotherKey(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "project", "create", "OPS", "Operations")
	out := mustCLI(t, "import", "linear", linearSample, "--project", "ops")
	if !strings.Contains(out, "Imported 9 tickets into OPS (Operations).") {
		t.Fatalf("%s", out)
	}
	if tk := showJSON(t, "OPS-12"); tk.Title != "Try the new parser" {
		t.Fatalf("%+v", tk)
	}
	if got := projectKeys(t); !slices.Equal(got, []string{"OPS", "VAQ"}) {
		t.Fatal(got)
	}
	// A project with any ticket is refused.
	mustCLI(t, "ticket", "new", "VAQ", "Mine")
	if code, _, errOut := cli(t, "import", "linear", linearSample, "--project", "VAQ"); code != 1 || !strings.Contains(errOut, "VAQ already has tickets") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// Review Focus 5 / N-R4: a missing required column refuses the import and
// names each one; an unknown column is ignored and listed.
func TestImportLinearColumns(t *testing.T) {
	cliEnv(t)
	file := writeCSV(t, "ID,Team,Description,Status,Labels\nENG-1,Engineering,,Todo,\n")
	code, _, errOut := cli(t, "import", "linear", file)
	if code != 1 || !strings.Contains(errOut, "is missing the columns Title and Priority") || !strings.Contains(errOut, "Nothing was imported.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if got := projectKeys(t); !slices.Equal(got, []string{"VAQ"}) {
		t.Fatal(got)
	}

	// Header names are matched without regard to case (Linear's own
	// importer reads "Id"), and a byte-order mark is skipped.
	file = writeCSV(t, "\ufeffId,Team,Title,Description,Status,Priority,Labels,Sprint goal,Story points\n"+
		"ENG-4,Engineering,One,,Todo,Low,,Ship it,3\n")
	out := mustCLI(t, "import", "linear", file)
	if !strings.Contains(out, "Imported 1 ticket into ENG (Engineering), a new project.") ||
		!strings.Contains(out, "Unknown columns, ignored: Sprint goal, Story points.") ||
		strings.Contains(out, "Columns not imported") {
		t.Fatalf("%s", out)
	}
	if tk := showJSON(t, "ENG-4"); tk.Priority != 4 {
		t.Fatalf("%+v", tk)
	}

	var res linearResult
	file = writeCSV(t, "ID,Team,Title,Description,Status,Priority,Labels,Extra\nDOC-7,Docs,Write it,,Done,Medium,\"Writing, Guide\",x\n")
	if err := json.Unmarshal([]byte(mustCLI(t, "import", "linear", file, "--json")), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 1 || res.Projects[0].Key != "DOC" || !res.Projects[0].Created || res.Projects[0].Tickets != 1 ||
		!slices.Equal(res.Unknown, []string{"Extra"}) || !slices.Equal(res.NotInCSV, []string{"comments", "relations", "attachments"}) {
		t.Fatalf("%+v", res)
	}
}

// Every row is checked before anything is written; each problem names its
// line.
func TestImportLinearRefusesBadRows(t *testing.T) {
	cliEnv(t)
	head := "ID,Team,Title,Description,Status,Priority,Labels\n"
	for _, c := range []struct{ body, want string }{
		{"ENG-1,Engineering,One,,Todo,Whenever,\n", `line 2: unknown priority "Whenever"`},
		{"twelve,Engineering,One,,Todo,Low,\n", `line 2: "twelve" isn't an issue ID like ENG-123`},
		{"ENG-1,Engineering,One,,Todo,Low,\nENG-1,Engineering,Two,,Todo,Low,\n", "line 3: ENG-1 is also on line 2"},
		{"ENG-1,Engineering,,,Todo,Low,\n", "line 2: ENG-1 has no title"},
		{"ENG-1,Engineering,One,,,Low,\n", "line 2: ENG-1 has no status"},
		{"ENG-1,Engineering,One,,Todo,Low,\"" + strings.Repeat("x", 41) + "\"\n", "line 2: the label " + strings.Repeat("x", 41) + " is longer than 40 characters"},
		{"E-1,Engineering,One,,Todo,Low,\n", "E isn't a project key here: give --project KEY"},
		{"", "has no issues"},
	} {
		code, _, errOut := cli(t, "import", "linear", writeCSV(t, head+c.body))
		if code != 1 || !strings.Contains(errOut, c.want) || !strings.Contains(errOut, "Nothing was imported.") {
			t.Fatalf("%q: %d %q", c.body, code, errOut)
		}
	}
	if got := projectKeys(t); !slices.Equal(got, []string{"VAQ"}) {
		t.Fatal(got)
	}

	// Two teams make two projects; --project then can't say which.
	two := writeCSV(t, head+"ENG-1,Engineering,One,,Todo,Low,\nDES-2,Design,Two,,Todo,Low,\n")
	if code, _, errOut := cli(t, "import", "linear", two, "--project", "ONE"); code != 2 || !strings.Contains(errOut, "has issues from 2 teams (DES, ENG)") {
		t.Fatalf("%d %q", code, errOut)
	}
	out := mustCLI(t, "import", "linear", two)
	if !strings.Contains(out, "Imported 1 ticket into DES (Design), a new project.") || !strings.Contains(out, "Imported 1 ticket into ENG (Engineering), a new project.") {
		t.Fatalf("%s", out)
	}

	// Usage.
	if code, _, _ := cli(t, "import", "linear"); code != 2 {
		t.Fatal(code)
	}
	if code, _, errOut := cli(t, "import", "linear", filepath.Join(t.TempDir(), "none.csv")); code != 1 || !strings.Contains(errOut, "can't read") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// A ticket the server refuses part way stops the import and says where;
// the tickets before it stay. An archived project is refused up front.
func TestImportLinearStopsAtARefusedTicket(t *testing.T) {
	e := cliEnv(t)
	head := "ID,Team,Title,Description,Status,Priority,Labels\n"
	file := writeCSV(t, head+"ENG-1,Engineering,One,,Todo,Low,\nENG-2,Engineering,Two,\"bad \x01 byte\",Todo,Low,\nENG-3,Engineering,Three,,Todo,Low,\n")
	code, _, errOut := cli(t, "import", "linear", file)
	if code != 1 || !strings.Contains(errOut, "stopped at ENG-2 (line 3)") || !strings.Contains(errOut, "ENG has the 1 ticket before it") {
		t.Fatalf("%d %q", code, errOut)
	}
	if tk := showJSON(t, "ENG-1"); tk.Title != "One" {
		t.Fatalf("%+v", tk)
	}
	if code, _, _ := cli(t, "ticket", "show", "ENG-3"); code != 1 {
		t.Fatal("ENG-3 was made after the stop")
	}

	old, err := e.Deps.Core.CreateProject(context.Background(), e.Deps.Actor.ID, core.ProjectInput{Key: "OLD", Name: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Deps.Core.SetProjectArchived(context.Background(), e.Deps.Actor.ID, old.ID, true); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = cli(t, "import", "linear", writeCSV(t, head+"ENG-1,Engineering,One,,Todo,Low,\n"), "--project", "OLD")
	if code != 1 || !strings.Contains(errOut, "OLD is archived") || !strings.Contains(errOut, "Nothing was imported.") {
		t.Fatalf("%d %q", code, errOut)
	}
}
