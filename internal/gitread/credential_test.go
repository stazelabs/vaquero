package gitread_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// Fetch uses the user's own credential helpers, from global and system
// config, plain and URL-scoped, and never one the repo configures: its
// credential.helper, its credential.<url>.helper or one it includes (M5).
func TestFetchUsesOnlyTheUsersCredentialHelpers(t *testing.T) {
	gittest.Need(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := func(name string) string {
		p := filepath.Join(dir, name+".sh")
		body := "#!/bin/sh\necho \"" + name + " $1\" >> '" + marker + "'\ncat >/dev/null\nexit 0\n"
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return "!" + p
	}
	ran := func() string {
		b, _ := os.ReadFile(marker)
		return string(b)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	// The user's config: a plain helper and one for this server's URL,
	// and an empty system config.
	globalHelper, globalURLHelper := script("global"), script("global-url")
	globalCfg := filepath.Join(dir, "gitconfig")
	cfg := "[credential]\n\thelper = " + globalHelper + "\n[credential \"" + srv.URL + "\"]\n\thelper = " + globalURLHelper + "\n"
	if err := os.WriteFile(globalCfg, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "home", ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	// The repo's config: three helpers of its own.
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Git("remote", "add", "origin", srv.URL+"/o/r.git")
	r.Git("config", "credential.helper", script("local"))
	r.Git("config", "credential."+srv.URL+".helper", script("local-url"))
	include := filepath.Join(dir, "include.cfg")
	if err := os.WriteFile(include, []byte("[credential]\n\thelper = "+script("local-include")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Git("config", "include.path", include)

	// The traps are live: plain git runs all three repo helpers.
	r.Try("fetch", "-q", "origin")
	for _, name := range []string{"local get", "local-url get", "local-include get"} {
		if !strings.Contains(ran(), name) {
			t.Fatalf("plain git fetch didn't run %s: %q", name, ran())
		}
	}
	os.Remove(marker)

	rs := gitread.New()
	rec := gittest.Record(rs)
	g, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Fetch(t.Context()); err == nil {
		t.Fatal("fetched from a server refusing every request")
	}
	got := ran()
	if strings.Contains(got, "local") {
		t.Fatalf("a repo's credential helper ran: %q", got)
	}
	for _, name := range []string{"global get", "global-url get"} {
		if !strings.Contains(got, name) {
			t.Fatalf("the user's helper %s didn't run: %q", name, got)
		}
	}

	// The helper set handed to git is exactly the user's, after the reset.
	if n := rec.Count("config"); n != 2 {
		t.Fatalf("%d helper reads", n)
	}
	fetches := 0
	for _, call := range rec.Calls() {
		if gitread.Subcommand(call) != "fetch" {
			continue
		}
		fetches++
		var helpers []string
		for i := 0; i+1 < len(call); i++ {
			if call[i] == "-c" && strings.HasPrefix(strings.ToLower(call[i+1]), "credential.") && strings.Contains(strings.ToLower(call[i+1]), "helper=") {
				helpers = append(helpers, call[i+1])
			}
		}
		want := []string{"credential.helper=", "credential.helper=" + globalHelper, "credential." + srv.URL + ".helper=" + globalURLHelper}
		if !slices.Equal(helpers, want) {
			t.Fatalf("fetch's helpers:\n%q\nwant\n%q", helpers, want)
		}
		if !slices.Contains(call, "credential.interactive=never") {
			t.Fatal("fetch without credential.interactive=never")
		}
	}
	if fetches != 1 {
		t.Fatalf("%d fetches", fetches)
	}
}
