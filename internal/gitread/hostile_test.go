package gitread_test

import (
	"context"
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

// trap is a script that touches marker when run, and a check that it
// never ran.
type trap struct {
	script, marker string
}

func newTrap(t *testing.T) trap {
	t.Helper()
	dir := t.TempDir()
	tr := trap{script: filepath.Join(dir, "evil.sh"), marker: filepath.Join(dir, "ran")}
	body := "#!/bin/sh\necho \"$0 $*\" >> '" + tr.marker + "'\nexit 0\n"
	if err := os.WriteFile(tr.script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return tr
}

func (tr trap) sprung() string {
	b, err := os.ReadFile(tr.marker)
	if err != nil {
		return ""
	}
	return string(b)
}

func (tr trap) reset(t *testing.T) {
	t.Helper()
	if err := os.Remove(tr.marker); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// arm writes a .git/config (and attributes and hooks) that runs the trap
// from every place a repo's config can name a program.
func arm(t *testing.T, r *gittest.R, tr trap) {
	t.Helper()
	hooks := filepath.Join(t.TempDir(), "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"reference-transaction", "post-checkout", "post-merge", "pre-auto-gc", "post-rewrite", "post-index-change", "fsmonitor-watchman", "pre-commit"} {
		if err := os.Symlink(tr.script, filepath.Join(hooks, h)); err != nil {
			t.Fatal(err)
		}
	}
	s := tr.script
	signers := filepath.Join(t.TempDir(), "allowed_signers")
	if err := os.WriteFile(signers, []byte("a@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"core.fsmonitor", s},
		{"core.pager", s},
		{"pager.log", s},
		{"pager.show", s},
		{"pager.diff", s},
		{"pager.tag", s},
		{"core.hooksPath", hooks},
		{"core.askPass", s},
		{"core.sshCommand", s},
		{"core.gitProxy", s},
		{"core.alternateRefsCommand", s},
		{"core.editor", s},
		{"diff.external", s},
		{"diff.evil.command", s},
		{"diff.evil.textconv", s},
		{"filter.evil.clean", s},
		{"filter.evil.smudge", s},
		{"log.showSignature", "true"},
		{"gpg.program", s},
		{"gpg.ssh.program", s},
		{"gpg.x509.program", s},
		{"gpg.ssh.allowedSignersFile", signers},
		{"credential.helper", "!" + s},
		{"remote.origin.uploadpack", s},
		{"protocol.ext.allow", "always"},
		{"http.saveCookies", "true"},
		{"http.cookieFile", filepath.Join(filepath.Dir(tr.marker), "cookies")},
		{"gc.auto", "1"},
		{"maintenance.auto", "true"},
		{"fetch.recurseSubmodules", "true"},
	} {
		r.Git("config", kv[0], kv[1])
	}
	r.Write(".git/info/attributes", "* diff=evil filter=evil\n")
}

// A repo's own config is untrusted: no read, and no fetch, runs anything it
// names.
func TestHostileRepoConfig(t *testing.T) {
	tr := newTrap(t)
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	c := seed.WriteCommit("docs/a.md", "one\n", "one (VAQ-1)")
	seed.Tag("v1")
	seed.Git("push", "-q", "origin", "main", "v1")

	r := gittest.Clone(t, bare)
	r.Branch("feat")
	r.WriteCommit("docs/b.md", "b\n", "feat (VAQ-1)")
	r.Checkout("main")
	r.WriteCommit("docs/a.md", "two\n", "two (VAQ-1)")
	arm(t, r, tr)

	// The trap is live: git without Vaquero's guards runs it.
	if _, err := r.Try("diff", "HEAD~1", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if tr.sprung() == "" {
		t.Fatal("the trap didn't spring for a plain git diff; the test proves nothing")
	}
	tr.reset(t)
	r.Try("status")
	if tr.sprung() == "" {
		t.Fatal("the trap didn't spring for a plain git status (core.fsmonitor)")
	}
	tr.reset(t)

	rs := gitread.New()
	rec := gittest.Record(rs)
	ctx := t.Context()
	g, err := rs.Open(ctx, r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", what, err)
		}
		if got := tr.sprung(); got != "" {
			t.Fatalf("%s ran the repo's program: %s", what, got)
		}
	}
	_, err = g.Head(ctx)
	check("Head", err)
	_, err = g.Show(ctx, "main", "docs/a.md")
	check("Show", err)
	_, err = g.Blob(ctx, c, "docs/a.md")
	check("Blob", err)
	_, _, err = g.Follow(ctx, "docs/a.md", c)
	check("Follow", err)
	_, err = g.PathLog(ctx, "docs/a.md", "", 10)
	check("PathLog", err)
	_, err = g.Mentions(ctx, "VAQ-1", 10)
	check("Mentions", err)
	_, err = g.BranchCommits(ctx, "feat", 10)
	check("BranchCommits", err)
	_, err = g.Merged(ctx, "feat")
	check("Merged", err)
	_, err = g.TagExists(ctx, "v1")
	check("TagExists", err)
	_, err = g.Diffstat(ctx, []string{c, r.Head()})
	check("Diffstat", err)
	_, err = g.FirstTagContaining(ctx, c)
	check("FirstTagContaining", err)
	_, _, _, err = g.Compare(ctx, "main", "origin/main")
	check("Compare", err)
	_, _, err = g.RemoteTag(ctx, "v1")
	check("RemoteTag", err)
	_, err = g.Remotes(ctx)
	check("Remotes", err)
	_, err = g.RemoteURL(ctx)
	check("RemoteURL", err)
	g.LastFetch()
	check("LastFetch", nil)
	// Diffs as later reads will run them: the guards keep the repo's
	// diff.external, diff driver and textconv from running.
	head := r.Head()
	for _, args := range [][]string{
		{"diff", "--no-ext-diff", "--no-textconv", "--stat", c, head, "--"},
		{"diff", "--no-ext-diff", "--no-textconv", c, head, "--", "docs/a.md"},
		{"log", "-p", "--no-ext-diff", "--no-textconv", "--no-show-signature", "-2"},
		{"show", "--no-ext-diff", "--no-textconv", "--no-show-signature", head},
	} {
		_, err := gitread.RunCommand(rs, ctx, g.Path, args[0], args[1:]...)
		check("git "+strings.Join(args, " "), err)
	}
	// Signed commits, one per signature program: a format atom that
	// verifies a signature runs gpg.program, gpg.ssh.program or
	// gpg.x509.program unless the guards override them.
	for _, armor := range []string{"PGP SIGNATURE", "SSH SIGNATURE", "SIGNED MESSAGE"} {
		signed := signedCommit(t, r, armor)
		r.Try("log", "-1", "--format=%G?", signed)
		if tr.sprung() == "" {
			t.Fatalf("the trap didn't spring for a plain signature check of a %s", armor)
		}
		tr.reset(t)
		for _, format := range []string{"--format=%G?", "--format=%GG", "--format=%GS"} {
			gitread.RunCommand(rs, ctx, g.Path, "log", "-1", "--no-ext-diff", "--no-textconv", "--no-show-signature", format, signed)
			check("git log "+format+" of a "+armor, nil)
		}
	}
	// O-R6: no read touches the network.
	if n := rec.Count("fetch"); n != 0 {
		t.Fatalf("%d fetches among the reads", n)
	}
	check("Fetch from a local remote", g.Fetch(ctx))
	if on, known, err := g.RemoteTag(ctx, "v1"); err != nil || !on || !known {
		t.Fatalf("the fetch didn't fetch: %v %v %v", on, known, err)
	}

	// A remote asking for a password: the repo's credential helper and
	// askpass never run, and nothing prompts.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	r.Git("remote", "add", "auth", srv.URL+"/o/r.git")
	ga, err := rs.Open(ctx, r.Dir, "main", "auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := ga.Fetch(ctx); err == nil {
		t.Fatal("fetched from a server refusing every request")
	}
	check("Fetch from a remote asking for a password", nil)

	// A remote helper URL that would run a command, allowed by the repo.
	r.Git("remote", "add", "ext", "ext::sh -c touch% "+tr.marker)
	ge, err := rs.Open(ctx, r.Dir, "main", "ext")
	if err != nil {
		t.Fatal(err)
	}
	if err := ge.Fetch(ctx); err == nil {
		t.Fatal("fetched through ext::")
	}
	check("Fetch through ext::", nil)

	// A URL rewritten into ext:: by the repo's insteadOf.
	r.Git("config", "url.ext::sh -c touch% "+tr.marker+"#.insteadOf", "https://rewritten.example/")
	r.Git("remote", "add", "rw", "https://rewritten.example/o/r.git")
	gr, err := rs.Open(ctx, r.Dir, "main", "rw")
	if err != nil {
		t.Fatal(err)
	}
	if err := gr.Fetch(ctx); err == nil {
		t.Fatal("fetched through a rewritten URL")
	}
	check("Fetch through insteadOf", nil)
	if _, err := os.Stat(filepath.Join(filepath.Dir(tr.marker), "cookies")); err == nil {
		t.Fatal("a fetch wrote the repo's cookie file")
	}

	for _, call := range rec.Calls() {
		hardened(t, call)
	}
}

// hardened checks one command carries every guard.
func hardened(t *testing.T, args []string) {
	t.Helper()
	for _, read := range gitread.HelperReads {
		if slices.Equal(args, read) {
			return // the fixed read of the user's own credential.helper
		}
	}
	if len(args) < 2 || args[0] != "-C" {
		t.Fatalf("not git -C: %q", args)
	}
	for _, want := range []string{"core.fsmonitor=false", "core.hooksPath=/dev/null", "credential.interactive=never", "credential.helper=", "core.askPass="} {
		if !slices.Contains(args, want) {
			t.Errorf("%s missing from %q", want, args)
		}
	}
	switch sub := gitread.Subcommand(args); sub {
	case "log", "show", "diff":
		want := []string{"--no-textconv", "--no-ext-diff"}
		if sub != "diff" {
			want = append(want, "--no-show-signature")
		}
		for _, w := range want {
			if !slices.Contains(args, w) {
				t.Errorf("%s missing from git %s: %q", w, sub, args)
			}
		}
	case "":
		t.Errorf("no subcommand in %q", args)
	}
}

// Every command runs with the guards in its environment, and only fetch
// gets GIT_SSH_COMMAND.
func TestCommandEnvironment(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.Clone(t, bare)
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor'='/bin/false'")
	t.Setenv("GIT_ASKPASS", "/bin/false")
	t.Setenv("SSH_ASKPASS", "/bin/false")
	rs := gitread.New()
	var envs [][]string
	var subs []string
	rs.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args, env []string, limit int) ([]byte, error) {
			envs = append(envs, slices.Clone(env))
			subs = append(subs, gitread.Subcommand(args))
			return next(ctx, dir, args, env, limit)
		}
	})
	g, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Head(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := g.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	sawFetch := false
	for i, env := range envs {
		for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C"} {
			if !slices.Contains(env, want) {
				t.Errorf("git %s: %s missing", subs[i], want)
			}
		}
		for _, e := range env {
			name, _, _ := strings.Cut(e, "=")
			if (strings.HasPrefix(name, "GIT_") && !slices.Contains([]string{"GIT_TERMINAL_PROMPT", "GIT_OPTIONAL_LOCKS", "GIT_PAGER", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM"}, name)) || name == "SSH_ASKPASS" {
				t.Errorf("git %s: %s passed through", subs[i], e)
			}
		}
		ssh := slices.Contains(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		if subs[i] == "fetch" {
			sawFetch = true
			if !ssh {
				t.Error("fetch without GIT_SSH_COMMAND")
			}
		} else if ssh || slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "GIT_SSH_COMMAND=") }) {
			t.Errorf("git %s has GIT_SSH_COMMAND", subs[i])
		}
	}
	if !sawFetch {
		t.Fatal("no fetch recorded")
	}
}

// signedCommit writes a commit object carrying a (fake) signature in
// armor ("PGP SIGNATURE", "SSH SIGNATURE" or "SIGNED MESSAGE", which pick
// gpg.program, gpg.ssh.program or gpg.x509.program) and returns its hash.
func signedCommit(t *testing.T, r *gittest.R, armor string) string {
	t.Helper()
	tree := r.Git("rev-parse", "HEAD^{tree}")
	obj := "tree " + tree + "\nparent " + r.Head() + "\n" +
		"author A <a@example.com> 1795708800 +0000\ncommitter A <a@example.com> 1795708800 +0000\n" +
		"gpgsig -----BEGIN " + armor + "-----\n \n iQEzBAABCAAdFiEE\n -----END " + armor + "-----\n\nsigned " + armor + "\n"
	path := filepath.Join(t.TempDir(), "commit")
	if err := os.WriteFile(path, []byte(obj), 0o644); err != nil {
		t.Fatal(err)
	}
	return r.Git("hash-object", "-t", "commit", "-w", path)
}
