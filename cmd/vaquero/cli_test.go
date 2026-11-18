package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// cliEnv is a webtest server with project VAQ, and a saved profile "t"
// pointing at it, selected through VAQUERO_PROFILE.
func cliEnv(t *testing.T) *webtest.Env {
	t.Helper()
	e := webtest.New(t)
	if _, err := e.Deps.Core.CreateProject(context.Background(), e.Deps.Actor.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"}); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	cfg := client.Config{Profiles: []client.Profile{{Name: "t", URL: e.Server.URL, Token: e.Token(t)}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAQUERO_PROFILE", "t")
	return e
}

func cli(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func mustCLI(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := cli(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\n%s", args, code, errOut)
	}
	return out
}

func TestCLITicketLifecycle(t *testing.T) {
	cliEnv(t)
	if out := mustCLI(t, "ticket", "new", "VAQ", "Build the board", "--tag", "ui", "--priority", "high"); strings.TrimSpace(out) != "VAQ-1" {
		t.Fatalf("%q", out)
	}
	mustCLI(t, "ticket", "new", "VAQ", "Second")
	show := mustCLI(t, "ticket", "show", "vaq-1")
	for _, want := range []string{"VAQ-1", "Build the board", "Todo", "High", "ui"} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %q:\n%s", want, show)
		}
	}
	mustCLI(t, "ticket", "edit", "VAQ-1", "--status", "in progress", "--add-tag", "board")
	mustCLI(t, "ticket", "move", "VAQ-1", "In review")
	mustCLI(t, "comment", "VAQ-1", "Looks good.")
	mustCLI(t, "link", "VAQ-1", "blocks", "VAQ-2")
	var tk struct {
		Key, Status string
		Tags        []string
		Links       []struct{ Kind, Key string }
		Comments    []struct{ Body string }
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", "VAQ-1", "--json")), &tk); err != nil {
		t.Fatal(err)
	}
	if tk.Status != "In review" || len(tk.Tags) != 2 || len(tk.Links) != 1 || tk.Links[0].Key != "VAQ-2" || len(tk.Comments) != 1 {
		t.Fatalf("%+v", tk)
	}
	mustCLI(t, "unlink", "VAQ-1", "VAQ-2")
	list := mustCLI(t, "tickets", "VAQ")
	if !strings.Contains(list, "VAQ-1") || !strings.Contains(list, "VAQ-2") {
		t.Fatalf("%s", list)
	}
	if out := mustCLI(t, "search", "board"); !strings.Contains(out, "VAQ-1") {
		t.Fatalf("%s", out)
	}
	if out := mustCLI(t, "activity", "--limit", "3"); !strings.Contains(out, "VAQ-1") {
		t.Fatalf("%s", out)
	}
	if out := mustCLI(t, "projects"); !strings.Contains(out, "VAQ") {
		t.Fatalf("%s", out)
	}
}

// Review Focus 1 at the CLI.
func TestCLIConflictExit3(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "x")
	mustCLI(t, "ticket", "edit", "VAQ-1", "--title", "first", "--version", "1")
	code, out, errOut := cli(t, "ticket", "edit", "VAQ-1", "--title", "second", "--version", "1")
	if code != 3 || !strings.Contains(errOut, "VAQ-1 changed since you read it.") || !strings.Contains(out, "first") {
		t.Fatalf("exit %d\nout %s\nerr %s", code, out, errOut)
	}
}

func TestCLIBodyFromStdin(t *testing.T) {
	cliEnv(t)
	stdin = strings.NewReader("Line one\nLine two\n")
	defer func() { stdin = nil }()
	mustCLI(t, "ticket", "new", "VAQ", "From stdin", "--body", "-")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "Line two") {
		t.Fatalf("%s", out)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	cliEnv(t)
	for _, args := range [][]string{{"ticket"}, {"ticket", "show"}, {"ticket", "frobnicate", "VAQ-1"}, {"link", "VAQ-1", "eats", "VAQ-2"}, {"ticket", "new", "VAQ", "x", "--nope"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: exit %d %q", args, code, errOut)
		}
	}
	if code, _, errOut := cli(t, "ticket", "show", "VAQ-99"); code != 1 || !strings.Contains(errOut, "No ticket VAQ-99.") {
		t.Errorf("not found: %d %q", code, errOut)
	}
}

func TestCLIProfileCommands(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "profile", "add", "team", "https://vq.example", "vq_x")
	if out := mustCLI(t, "profile", "list"); !strings.Contains(out, "team") || !strings.Contains(out, "https://vq.example") || strings.Contains(out, "vq_x") {
		t.Fatalf("%s", out)
	}
	mustCLI(t, "profile", "use", "team")
	mustCLI(t, "profile", "remove", "team")
	if code, _, _ := cli(t, "profile", "use", "gone"); code != 1 {
		t.Fatalf("%d", code)
	}
}

func TestCLITokens(t *testing.T) {
	cliEnv(t)
	out := mustCLI(t, "token", "create", "ci")
	if !strings.HasPrefix(strings.TrimSpace(out), "vq_") {
		t.Fatalf("%q", out)
	}
	prefix := strings.TrimSpace(out)[:9]
	if list := mustCLI(t, "token", "list"); !strings.Contains(list, "ci") || !strings.Contains(list, prefix) {
		t.Fatalf("%s", list)
	}
	mustCLI(t, "token", "revoke", prefix)
}

func TestCLIUpgradeMessage(t *testing.T) {
	cliEnv(t)
	old := middleware.MinClient
	middleware.MinClient = "99.0.0"
	defer func() { middleware.MinClient = old }()
	code, _, errOut := cli(t, "projects")
	if code != 0 || !strings.Contains(errOut, "needs vaquero 99.0.0 or newer") {
		t.Fatalf("%d %q", code, errOut)
	}
}
