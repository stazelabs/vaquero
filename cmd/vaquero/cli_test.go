package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/version"
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

// VAQ-61: profile add reads the token from standard input for "-", so it
// stays out of the shell's history and the process list, and refuses a URL
// that isn't an http or https server address.
func TestCLIProfileAddTokenFromStdinAndURLCheck(t *testing.T) {
	cliEnv(t)
	old := stdin
	stdin = strings.NewReader("vq_fromstdin\n")
	t.Cleanup(func() { stdin = old })
	mustCLI(t, "profile", "add", "team", "https://vq.example/", "-")
	cfg, err := client.Load()
	if err != nil {
		t.Fatal(err)
	}
	var team client.Profile
	for _, p := range cfg.Profiles {
		if p.Name == "team" {
			team = p
		}
	}
	if team.Token != "vq_fromstdin" || team.URL != "https://vq.example" {
		t.Fatalf("%+v", cfg.Profiles)
	}
	for _, bad := range []string{"vq.example", "ftp://vq.example", "https://", "https://vq.example/?x=1", "https://vq.example/#top"} {
		if code, _, errOut := cli(t, "profile", "add", "other", bad, "vq_x"); code != 1 || !strings.Contains(errOut, "isn't a server address") {
			t.Errorf("%s: exit %d %q", bad, code, errOut)
		}
	}
	stdin = strings.NewReader("")
	if code, _, errOut := cli(t, "profile", "add", "other", "https://vq.example", "-"); code != 1 || !strings.Contains(errOut, "No token on standard input.") {
		t.Errorf("empty stdin: exit %d %q", code, errOut)
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
	oldV := version.Version
	version.Version = "1.0.0" // a release: dev builds are never told to upgrade
	defer func() { version.Version = oldV }()
	code, _, errOut := cli(t, "projects")
	if code != 0 || !strings.Contains(errOut, "needs vaquero 99.0.0 or newer") {
		t.Fatalf("%d %q", code, errOut)
	}
	// VAQ-61: once per run, however many requests it makes.
	var out, errBuf bytes.Buffer
	c := newCmd(&out, &errBuf, "x")
	if _, err := c.parse([]string{"--profile", "t"}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := c.call(t.Context(), "GET", "/api/v1/projects", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(errBuf.String(), "needs vaquero 99.0.0 or newer"); n != 1 {
		t.Fatalf("told %d times: %q", n, errBuf.String())
	}
}

// Text starting with "-" (a markdown list) goes through after --, which
// ends flag parsing for everything after it.
func TestCLIDashText(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "x")
	mustCLI(t, "comment", "VAQ-1", "--", "- did the thing\n- and another")
	mustCLI(t, "comment", "--", "VAQ-1", "- also this")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "- did the thing") || !strings.Contains(out, "- also this") {
		t.Fatalf("%s", out)
	}
	if code, _, errOut := cli(t, "comment", "VAQ-1", "- oops"); code != 2 || !strings.Contains(errOut, "put -- before it") {
		t.Fatalf("%d %q", code, errOut)
	}
}

func TestCLIActivityNewest(t *testing.T) {
	cliEnv(t)
	for range 30 {
		mustCLI(t, "ticket", "new", "VAQ", "t")
	}
	var evs []struct{ Seq int64 }
	if err := json.Unmarshal([]byte(mustCLI(t, "activity", "--limit", "5", "--json")), &evs); err != nil {
		t.Fatal(err)
	}
	var all []struct{ Seq int64 }
	json.Unmarshal([]byte(mustCLI(t, "activity", "--limit", "500", "--json")), &all)
	if len(evs) != 5 || evs[4].Seq != all[len(all)-1].Seq || evs[0].Seq > evs[4].Seq {
		t.Fatalf("%v (newest %d)", evs, all[len(all)-1].Seq)
	}
	for _, bad := range []string{"0", "-5"} {
		if code, _, _ := cli(t, "activity", "--limit", bad); code != 2 {
			t.Errorf("--limit %s: exit %d", bad, code)
		}
	}
}

func TestCLIProjectEditNeedsFlags(t *testing.T) {
	cliEnv(t)
	if code, _, _ := cli(t, "project", "edit", "VAQ", "New name"); code != 2 {
		t.Fatalf("extra argument accepted: exit %d", code)
	}
	mustCLI(t, "project", "edit", "VAQ", "--name", "New name")
	if out := mustCLI(t, "project", "show", "VAQ"); !strings.Contains(out, "New name") {
		t.Fatalf("%s", out)
	}
}

// A 409 conflict exits 3 even when the answer carries no current object
// (a project key change, say): scripts key on the code, not the body.
func TestCLIConflictExitsThreeWithoutCurrent(t *testing.T) {
	cliEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"conflict","message":"Someone else changed this."}}`))
	}))
	defer srv.Close()
	cfg := client.Config{Profiles: []client.Profile{{Name: "fake", URL: srv.URL, Token: "vq_x"}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := cli(t, "ticket", "edit", "VAQ-1", "--title", "x", "--profile", "fake")
	if code != 3 || !strings.Contains(errOut, "Someone else changed this.") {
		t.Fatalf("exit %d %q", code, errOut)
	}
}

func TestCLIVersionMustBePositive(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "x")
	for _, bad := range []string{"0", "-3"} {
		if code, _, errOut := cli(t, "ticket", "edit", "VAQ-1", "--title", "y", "--version", bad); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("--version %s: exit %d %q", bad, code, errOut)
		}
	}
}

// VAQ-61: ticket new has no version to check and no tags to remove.
func TestCLINewRefusesEditOnlyFlags(t *testing.T) {
	cliEnv(t)
	for _, flag := range [][]string{{"--version", "2"}, {"--remove-tag", "ui"}} {
		args := append([]string{"ticket", "new", "VAQ", "x"}, flag...)
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, flag[0]+" is for ticket edit") {
			t.Errorf("%s: exit %d %q", flag[0], code, errOut)
		}
	}
}

func TestCLIAgentFlag(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "x")
	mustCLI(t, "comment", "VAQ-1", "From the flag.", "--agent", "builder")
	t.Setenv("VAQUERO_AGENT", "reviewer")
	mustCLI(t, "comment", "VAQ-1", "From the environment.")
	mustCLI(t, "comment", "VAQ-1", "The flag wins.", "--agent", "builder")
	var tk struct {
		Comments []struct{ Author, Body string }
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", "VAQ-1", "--json")), &tk); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range tk.Comments {
		got = append(got, c.Author)
	}
	if strings.Join(got, ",") != "builder,reviewer,builder" {
		t.Fatalf("%v", got)
	}
}

func TestCLIClaimFlow(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Ready work", "--priority", "high")
	if out := mustCLI(t, "ready", "VAQ"); !strings.Contains(out, "VAQ-1") || !strings.Contains(out, "Ready work") {
		t.Fatalf("ready: %s", out)
	}
	out := mustCLI(t, "claim", "VAQ-1", "--lease", "30m", "--agent", "builder")
	if !strings.Contains(out, "VAQ-1") || !strings.Contains(out, "12:30 UTC") {
		t.Fatalf("claim: %s", out)
	}
	if code, _, errOut := cli(t, "claim", "VAQ-1", "--agent", "reviewer"); code != 1 || !strings.Contains(errOut, "claimed by builder") {
		t.Fatalf("second claim: %d %q", code, errOut)
	}
	var tk struct {
		ClaimedBy      string `json:"claimed_by"`
		ClaimExpiresAt string `json:"claim_expires_at"`
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "heartbeat", "VAQ-1", "--lease", "1h", "--agent", "builder", "--json")), &tk); err != nil {
		t.Fatal(err)
	}
	if tk.ClaimedBy != "builder" || tk.ClaimExpiresAt != "2026-11-18T13:00:00.000Z" {
		t.Fatalf("%+v", tk)
	}
	if out := mustCLI(t, "ready", "VAQ", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("claimed ticket still ready: %s", out)
	}
	mustCLI(t, "release", "VAQ-1", "--agent", "builder")
	for _, bad := range [][]string{{"claim", "VAQ-1", "--lease", "soon"}, {"claim"}, {"ready", "--limit", "0"}} {
		if code, _, _ := cli(t, bad...); code != 2 {
			t.Errorf("%v: exit %d", bad, code)
		}
	}
}

// VAQ-56: tickets filters by milestone name, in any case.
func TestCLITicketsMilestoneFilter(t *testing.T) {
	e := cliEnv(t)
	ctx := context.Background()
	p, err := e.Deps.R.GetProjectByKey(ctx, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateMilestone(ctx, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	mustCLI(t, "ticket", "new", "VAQ", "In beta", "--milestone", "Beta")
	mustCLI(t, "ticket", "new", "VAQ", "Elsewhere")
	out := mustCLI(t, "tickets", "VAQ", "--milestone", "beta")
	if !strings.Contains(out, "In beta") || strings.Contains(out, "Elsewhere") {
		t.Fatalf("%s", out)
	}
	if code, _, errOut := cli(t, "tickets", "VAQ", "--milestone", "Gamma"); code == 0 || !strings.Contains(errOut, "Gamma") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
