// Package gitread is the only code in Vaquero that runs git (VAQ-136): the
// reads behind a project's repo settings, references, the file view, the
// readiness file pins and the trace, and the one optional fetch.
// TestOnlyGitreadRunsGit holds that nothing else runs it.
//
// The repository's content and its own config are untrusted. Every command
// is git -C {repo} with guard options that turn off what a repo's config
// could make git run: core.fsmonitor, hooks, pagers, the repo's credential
// helpers (Fetch re-adds the user's own, from global and system config
// only), askpass, signature programs (gpg.program, gpg.ssh.program,
// gpg.x509.program), the alternate refs command, cookie files, automatic gc
// and maintenance, submodule recursion and every transport but file, http,
// https and ssh; it also fixes what would change the output's shape
// (column.ui, color.ui, diff.relative) (guardConfig, guardOptions). The environment is the server's without any
// GIT_ variable or SSH_ASKPASS, plus GIT_TERMINAL_PROMPT=0,
// GIT_OPTIONAL_LOCKS=0, GIT_PAGER=cat and LC_ALL=C; fetch alone adds
// GIT_SSH_COMMAND="ssh -o BatchMode=yes", so a remote that wants a password
// fails instead of waiting, and every other command GIT_NO_LAZY_FETCH=1, so
// a read in a partial clone fails on a missing object instead of fetching
// it from the promisor remote (git 2.44+; under an older git Open refuses
// a partial clone, ErrPartialClone). New reads git version (the one
// command run outside a repo besides Fetch's helper reads), again on the
// next use when that failed; a git older than 2.31 (MinVersion) isn't used
// (Available false, ErrOldGit). Standard input is empty. A command has
// Timeout (Fetch: FetchTimeout), is killed with its process group when it
// runs over, and its output is read up to MaxOutput (a file shown: MaxShow).
//
// Only these subcommands run (Allowed): rev-parse, show, cat-file, log,
// merge-base, tag (only with --list), rev-list, for-each-ref, remote (only
// the list and get-url), ls-tree (only Blobs' one form: -z --full-tree, a
// commit hash, then "--" and ValidPaths), config (only Open's one
// promisor read, under a git older than 2.44, and again after a missing file in a repo not yet asked about; the answer is kept per repo) and diff, which read; and fetch, which only Fetch
// runs, with the two fixed reads of the user's credential helpers (git
// config --system and --global -z --get-regexp for credential.helper and
// credential.<url>.helper, never the repo's). diff, log and show run only
// with --no-ext-diff and
// --no-textconv (both on by default in git, so a repo's diff.external or
// textconv would run), and their positional arguments must be revisions,
// with paths only after "--" and each a ValidPath (two paths outside the
// work tree would turn diff into --no-index by itself). No option that
// writes a file (--output), reads outside the repo (--no-index, anywhere;
// -O and --orderfile on diff, log and show, with diff.orderFile pinned to
// /dev/null) or applies a repo's drivers (--textconv, --filters,
// --ext-diff) passes. The environment keeps GIT_CONFIG_GLOBAL,
// GIT_CONFIG_SYSTEM and GIT_CONFIG_NOSYSTEM, which only say where the
// user's own config is.
// Fetch is the one
// command that touches the network and the one that writes: remote-tracking
// refs under refs/remotes/{remote}/ and the remote's tags under
// refs/vaquero/remote-tags/{remote}/, never local branches, local tags or
// the work tree.
//
// Inputs are checked before they reach git (ValidPath, ValidRev); a
// refused input is ErrInvalid. A failing command is an *Error carrying
// git's first stderr line, cut to 200 characters.
package gitread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Limits on every command.
const (
	Timeout      = 5 * time.Second
	FetchTimeout = 60 * time.Second
	MaxOutput    = 1 << 20   // bytes of output read from any command
	MaxShow      = 512 << 10 // bytes of a file shown; past it, File.TooLarge
	maxLine      = 200       // characters of git's stderr line in an error
	maxStderr    = 4 << 10
)

// Errors.
var (
	ErrNoGit       = errors.New("gitread: git is not installed")
	ErrNotRepo     = errors.New("gitread: not a git work tree")
	ErrNoFile      = errors.New("gitread: no such file at that revision")
	ErrNoRev       = errors.New("gitread: no such revision")
	ErrNoRemote    = errors.New("gitread: no remote to fetch")
	ErrInvalid     = errors.New("gitread: not a valid path, revision or name")
	ErrRefused     = errors.New("gitread: refused, not a read-only git command")
	ErrOutputLimit = errors.New("gitread: output too large to read")
	// ErrOldGit is a git older than MinVersion: found, but not used.
	ErrOldGit = errors.New("gitread: git 2.31 or later is needed")
	// ErrVersionUnreadable is a git found whose version couldn't be read
	// (asked again on the next use): not an old git.
	ErrVersionUnreadable = errors.New("gitread: couldn't read git's version")
	// ErrPartialClone is a partial clone (or a repo naming a promisor
	// remote) under a git older than 2.44, which can't be kept from
	// fetching missing objects on a read.
	ErrPartialClone = errors.New("gitread: partial clones need git 2.44 or later")
	// ErrPartialRead is a read that needed an object a partial clone
	// doesn't have (a tree:0 clone's trees, say): not a missing file or
	// revision, and never fetched from the promisor remote (VAQ-166).
	ErrPartialRead = errors.New("gitread: this is a partial clone; Vaquero can't read that from it")
)

// PartialMessage is the sentence for ErrPartialRead, as a page, the API
// and the CLI show it.
const PartialMessage = "This is a partial clone; Vaquero can't read that from it."

// The git versions gitread needs: MinVersion for every read (older git
// lacks options the reads pass), NoLazyFetchVersion for a partial clone
// (GIT_NO_LAZY_FETCH, which keeps a read from fetching a missing object
// from the promisor remote; older git ignores the variable, so Open
// refuses a partial clone there).
var (
	MinVersion         = [2]int{2, 31}
	NoLazyFetchVersion = [2]int{2, 44}
)

// Error is a git command that failed: its subcommand, exit code (-1 when
// it didn't exit, as after a timeout) and the first line git wrote to
// stderr, cut to 200 characters.
type Error struct {
	Sub  string
	Code int
	Line string
	err  error
}

func (e *Error) Error() string {
	if e.Line != "" {
		return "git " + e.Sub + ": " + e.Line
	}
	if e.err != nil {
		return "git " + e.Sub + ": " + e.err.Error()
	}
	return fmt.Sprintf("git %s exited with %d", e.Sub, e.Code)
}

func (e *Error) Unwrap() error { return e.err }

// RunFunc runs git with args (everything after "git", the guards included)
// and env (the whole environment), reading at most limit bytes of output.
// dir is the repo the command reads. Wrap puts hooks around it.
type RunFunc func(ctx context.Context, dir string, args []string, env []string, limit int) ([]byte, error)

// Repos runs git for every project: where git is, the runner, and the
// fetches in flight (one per repo at a time).
type Repos struct {
	git string

	vmu      sync.Mutex
	readVer  func(*Repos) string // git version's output; "" when it failed
	version  [2]int              // major, minor; zero when unknown
	verKnown bool                // git version answered (VAQ-166: else asked again)

	mu       sync.Mutex
	runner   RunFunc
	fetching map[string]*flight
	fetched  map[string]bool // repo and remote fetched by this process
	failed   map[string]FetchFailure
	partial  map[string]bool // git dir -> its config makes it a partial clone (VAQ-193)
}

type flight struct {
	done    chan struct{}
	err     error
	waiters int // callers waiting on it besides the one that started it
}

// FetchFailure is why a repo's last fetch failed: git's first line, cut to
// 200 characters, and when. It is kept in memory, per repo, until a fetch
// succeeds.
type FetchFailure struct {
	Line string
	At   time.Time
}

// New finds git on PATH and reads its version once (git version). Without
// git Available is false and Open answers ErrNoGit; with a git older than
// MinVersion Available is false too and Open answers ErrOldGit; when the
// version can't be read it is read again on the next use (Open answers
// ErrVersionUnreadable meanwhile).
func New() *Repos { return newRepos(readVersion) }

// NewWithVersion is New as if git version had answered v (tests check
// what an older git turns off with it).
func NewWithVersion(v string) *Repos { return newRepos(func(*Repos) string { return v }) }

// newRepos is New with the version read by version (tests give one).
func newRepos(version func(*Repos) string) *Repos {
	r := &Repos{readVer: version, fetching: map[string]*flight{}, fetched: map[string]bool{}, failed: map[string]FetchFailure{}}
	r.runner = r.exec
	if p, err := exec.LookPath("git"); err == nil {
		r.git = p
		r.ver()
	}
	return r
}

// versionRead is the one command run before any repo: git version, from /.
var versionRead = []string{"-C", "/", "--no-pager", "version"}

// readVersion is git version's answer, or "" when it fails.
func readVersion(r *Repos) string {
	out, err := r.run(context.Background(), "/", versionRead, maxStderr)
	if err != nil {
		return ""
	}
	return string(out)
}

// parseVersion reads "git version 2.39.5 (Apple Git-154)" as {2, 39}.
func parseVersion(s string) ([2]int, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(s), "git version ")
	if !ok {
		return [2]int{}, false
	}
	num, _, _ := strings.Cut(rest, " ")
	parts := strings.SplitN(num, ".", 3)
	if len(parts) < 2 {
		return [2]int{}, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major < 0 || minor < 0 {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
}

// ver is git's version, read when first needed. A read that fails (git
// timed out, the machine was busy) is asked again on the next use, so one
// bad moment at startup doesn't leave git off for the process (VAQ-166); an
// answer, however odd, is kept.
func (r *Repos) ver() [2]int {
	if r.git == "" {
		return [2]int{}
	}
	r.vmu.Lock()
	defer r.vmu.Unlock()
	if !r.verKnown {
		out := r.readVer(r)
		r.version, _ = parseVersion(out)
		r.verKnown = out != ""
	}
	return r.version
}

// versionUnreadable reports whether git was found but git version has not
// answered (yet).
func (r *Repos) versionUnreadable() bool {
	if r.git == "" {
		return false
	}
	r.ver()
	r.vmu.Lock()
	defer r.vmu.Unlock()
	return !r.verKnown
}

// atLeast reports whether the git found is version v or later.
func (r *Repos) atLeast(v [2]int) bool {
	got := r.ver()
	return got[0] > v[0] || got[0] == v[0] && got[1] >= v[1]
}

// Available reports whether git was found and is MinVersion or later.
func (r *Repos) Available() bool { return r.git != "" && r.atLeast(MinVersion) }

// Problem is why git can't be used: ErrNoGit, ErrVersionUnreadable or
// ErrOldGit; nil when Available.
func (r *Repos) Problem() error {
	switch {
	case r.git == "":
		return ErrNoGit
	case r.versionUnreadable():
		return ErrVersionUnreadable
	case !r.Available():
		return ErrOldGit
	}
	return nil
}

// Missing is why repository features are off, as a sentence: "Git isn't
// installed.", "Couldn't read git's version." or "Git 2.31 or later is
// needed."; "" when Available.
func (r *Repos) Missing() string {
	switch {
	case r.git == "":
		return "Git isn't installed."
	case r.versionUnreadable():
		return "Couldn't read git's version."
	case !r.Available():
		return "Git 2.31 or later is needed."
	}
	return ""
}

// Wrap puts mw around the runner: tests record commands with it
// (gittest.Record) or answer some themselves. The checks that refuse a
// command run before any wrapper, so a wrapper never sees a refused one.
func (r *Repos) Wrap(mw func(RunFunc) RunFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runner = mw(r.runner)
}

// guardConfig is passed as -c to every command. An empty value resets a
// list (credential.helper) or turns a program off (core.askPass).
var guardConfig = []string{
	"core.fsmonitor=false",
	"core.hooksPath=/dev/null",
	"credential.interactive=never",
	"credential.helper=",
	// Fetch's user helpers go here (prefix).
	"core.askPass=",
	"core.pager=cat",
	"core.alternateRefsCommand=true",
	"core.untrackedCache=false",
	"log.showSignature=false",
	"gpg.program=false",
	"gpg.ssh.program=false",
	"gpg.x509.program=false",
	"column.ui=never",
	"color.ui=never",
	"diff.relative=false",
	"diff.orderFile=/dev/null",
	"http.saveCookies=false",
	"http.cookieFile=",
	"gc.auto=0",
	"maintenance.auto=false",
	"fetch.recurseSubmodules=false",
	"fetch.writeCommitGraph=false",
	"submodule.recurse=false",
	"protocol.allow=never",
	"protocol.file.allow=always",
	"protocol.http.allow=always",
	"protocol.https.allow=always",
	"protocol.ssh.allow=always",
	"protocol.git.allow=never",
	"protocol.ext.allow=never",
	"protocol.fd.allow=never",
}

// guardOptions follow the -c options, before the subcommand.
var guardOptions = []string{"--no-pager", "--literal-pathspecs", "--no-replace-objects", "--no-optional-locks"}

// prefix is every command's start: -C dir and the guards, with helpers
// (Fetch's user credential helpers, each "key=value") right after the
// credential.helper reset. That one empty value empties git's whole helper
// list, the repo's credential.<url>.helper entries and included ones too
// (checked with real git), so only what follows it counts.
func (r *Repos) prefix(dir string, helpers []string) []string {
	out := []string{"-C", dir}
	for _, c := range guardConfig {
		out = append(out, "-c", c)
		if c == "credential.helper=" {
			for _, h := range helpers {
				out = append(out, "-c", h)
			}
		}
	}
	return append(out, guardOptions...)
}

// helpersKey carries Fetch's user credential helpers on its context.
type helpersKey struct{}

func helpersFrom(ctx context.Context) []string {
	h, _ := ctx.Value(helpersKey{}).([]string)
	return h
}

// helperReads are the only config commands: the user's credential helpers
// (credential.helper and every credential.<url>.helper, in file order) from
// system and global config, run from / so no repo is in view. Never
// --local, never a repo's includes.
var helperReads = [][]string{
	{"-C", "/", "--no-pager", "config", "--system", "-z", "--get-regexp", `^credential\.(.+\.)?helper$`},
	{"-C", "/", "--no-pager", "config", "--global", "-z", "--get-regexp", `^credential\.(.+\.)?helper$`},
}

// diffLike run only with --no-ext-diff and --no-textconv.
var diffLike = []string{"diff", "log", "show"}

// Subcommand is the git subcommand in args (everything after "git"): the
// first argument that isn't a global option or an option's value.
func Subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" || a == "-c":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return ""
}

// allowed are the subcommands the runner runs; fetch only from Fetch.
var allowed = []string{"rev-parse", "show", "cat-file", "log", "merge-base", "tag", "rev-list", "for-each-ref", "remote", "diff", "ls-tree", "config", "fetch"}

// promisorRead is the one form git config runs in a repo (after the
// guards): the settings that make it a partial clone, as git itself would
// see them (every scope, includes followed), read by Open under a git
// older than NoLazyFetchVersion.
var promisorRead = []string{"--null", "--get-regexp", `^(extensions\.partialclone|remote\..+\.(promisor|partialclonefilter))$`}

// Allowed is the allow-list, in order.
func Allowed() []string { return slices.Clone(allowed) }

// fetchKey marks the context Fetch runs its command with.
type fetchKey struct{}

// check refuses a command outside the allow-list: args must be the guard
// prefix, then an allowed subcommand, then arguments that neither write
// nor apply a repo's drivers.
func (r *Repos) check(ctx context.Context, dir string, args []string) error {
	if slices.Equal(args, versionRead) {
		return nil
	}
	for _, read := range helperReads {
		if slices.Equal(args, read) {
			if ctx.Value(fetchKey{}) == nil {
				return fmt.Errorf("%w: git config runs only from Fetch", ErrRefused)
			}
			return nil
		}
	}
	helpers := helpersFrom(ctx)
	if len(helpers) > 0 && ctx.Value(fetchKey{}) == nil {
		return fmt.Errorf("%w: credential helpers only for Fetch", ErrRefused)
	}
	pre := r.prefix(dir, helpers)
	if len(args) <= len(pre) || !slices.Equal(args[:len(pre)], pre) {
		return fmt.Errorf("%w: missing the guard options", ErrRefused)
	}
	sub, rest := args[len(pre)], args[len(pre)+1:]
	if !slices.Contains(allowed, sub) {
		return fmt.Errorf("%w: %q", ErrRefused, sub)
	}
	if slices.Contains(rest, "--no-index") {
		return fmt.Errorf("%w: git %s --no-index", ErrRefused, sub)
	}
	if slices.Contains(diffLike, sub) {
		if err := checkDiffLike(sub, rest); err != nil {
			return err
		}
	}
	for _, a := range rest {
		if a == "--" || a == "--end-of-options" {
			break
		}
		switch {
		case a == "-o" || strings.HasPrefix(a, "--output"),
			a == "--textconv", a == "--filters", a == "--ext-diff", a == "--exec", a == "--no-index",
			strings.HasPrefix(a, "--upload-pack") && sub != "fetch",
			strings.HasPrefix(a, "--exec="):
			return fmt.Errorf("%w: git %s %s", ErrRefused, sub, a)
		}
	}
	switch sub {
	case "for-each-ref":
		// Reads of refs only: a --format and a ref prefix as the last
		// argument (VAQ-165).
		if len(rest) < 2 || !strings.HasPrefix(rest[len(rest)-1], "refs/") || !slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "--format=") }) {
			return fmt.Errorf("%w: git for-each-ref %q", ErrRefused, rest)
		}
	case "fetch":
		if ctx.Value(fetchKey{}) == nil {
			return fmt.Errorf("%w: fetch runs only from Fetch", ErrRefused)
		}
	case "tag":
		// Only listing: git tag NAME, -d, -a, -f and the rest write.
		if len(rest) == 0 || rest[0] != "--list" {
			return fmt.Errorf("%w: git tag without --list", ErrRefused)
		}
	case "ls-tree":
		// Only Blobs' form: -z --full-tree --end-of-options HASH -- PATH...
		if len(rest) < 6 || rest[0] != "-z" || rest[1] != "--full-tree" || rest[2] != "--end-of-options" || !isHash(rest[3]) || rest[4] != "--" {
			return fmt.Errorf("%w: git ls-tree %q", ErrRefused, rest)
		}
		for _, p := range rest[5:] {
			if !ValidPath(p) {
				return fmt.Errorf("%w: git ls-tree path %q", ErrRefused, p)
			}
		}
	case "config":
		// Only Open's partial-clone read.
		if !slices.Equal(rest, promisorRead) {
			return fmt.Errorf("%w: git config %q", ErrRefused, rest)
		}
	case "remote":
		// Only the list and get-url.
		if len(rest) != 0 && !(rest[0] == "get-url" && len(rest) == 2) {
			return fmt.Errorf("%w: git remote %s", ErrRefused, rest[0])
		}
	}
	return nil
}

// checkDiffLike holds diff, log and show to --no-ext-diff and
// --no-textconv, revisions as positional arguments, and ValidPaths after
// "--".
func checkDiffLike(sub string, rest []string) error {
	opts, paths := true, false
	var ext, tc bool
	for _, a := range rest {
		switch {
		case paths:
			if !ValidPath(a) {
				return fmt.Errorf("%w: git %s path %q", ErrRefused, sub, a)
			}
		case a == "--":
			paths = true
		case a == "--end-of-options":
			opts = false
		case opts && (strings.HasPrefix(a, "--orderfile") || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "O"))):
			// -O, -Ofile, --orderfile=…: git reads that file, anywhere.
			return fmt.Errorf("%w: git %s %s", ErrRefused, sub, a)
		case opts && strings.HasPrefix(a, "-"):
			ext = ext || a == "--no-ext-diff"
			tc = tc || a == "--no-textconv"
		default:
			if !revArg(a) {
				return fmt.Errorf("%w: git %s argument %q", ErrRefused, sub, a)
			}
		}
	}
	if !ext || !tc {
		return fmt.Errorf("%w: git %s without --no-ext-diff and --no-textconv", ErrRefused, sub)
	}
	return nil
}

// revArg is a revision argument: a ValidRev, ^rev, or rev..rev / rev...rev.
func revArg(a string) bool {
	a = strings.TrimPrefix(a, "^")
	for _, sep := range []string{"...", ".."} {
		if l, r, ok := strings.Cut(a, sep); ok {
			return ValidRev(l) && ValidRev(r)
		}
	}
	return ValidRev(a)
}

// userConfigEnv are the GIT_ variables kept: they only say which global and
// system config files are the user's, which the server's operator decides.
var userConfigEnv = []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM"}

// environ is the server's environment without any GIT_ variable or
// SSH_ASKPASS, plus the guards; fetch adds GIT_SSH_COMMAND.
func environ(fetch bool) []string {
	var env []string
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		if (strings.HasPrefix(name, "GIT_") && !slices.Contains(userConfigEnv, name)) || strings.HasPrefix(name, "SSH_ASKPASS") || name == "LC_ALL" {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C")
	if fetch {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	} else {
		// A read in a partial clone must not fetch a missing object from
		// the promisor remote (network, and a write). git 2.44+ honours
		// it; older git ignores it, so Open refuses a partial clone there.
		env = append(env, "GIT_NO_LAZY_FETCH=1")
	}
	return env
}

// call runs git sub args in dir with the guards.
func (r *Repos) call(ctx context.Context, dir, sub string, args []string, limit int) ([]byte, error) {
	return r.run(ctx, dir, append(append(r.prefix(dir, helpersFrom(ctx)), sub), args...), limit)
}

// run checks args, then runs them through the runner and its wrappers,
// under the command's timeout.
func (r *Repos) run(ctx context.Context, dir string, args []string, limit int) ([]byte, error) {
	if r.git == "" {
		return nil, ErrNoGit
	}
	if err := r.check(ctx, dir, args); err != nil {
		return nil, err
	}
	fetch := Subcommand(args) == "fetch"
	timeout := Timeout
	if fetch {
		timeout = FetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r.mu.Lock()
	runner := r.runner
	r.mu.Unlock()
	return runner(ctx, dir, args, environ(fetch), limit)
}

// exec is the real runner.
func (r *Repos) exec(ctx context.Context, _ string, args []string, env []string, limit int) ([]byte, error) {
	sub := Subcommand(args)
	cmd := exec.CommandContext(ctx, r.git, args...)
	cmd.Env = env
	cmd.Stdin = nil
	stderr := &capped{max: maxStderr}
	cmd.Stderr = stderr
	killGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &Error{Sub: sub, Code: -1, err: err}
	}
	if err := cmd.Start(); err != nil {
		return nil, &Error{Sub: sub, Code: -1, err: err}
	}
	buf, readErr := io.ReadAll(io.LimitReader(out, int64(limit)+1))
	over := len(buf) > limit
	if over {
		buf = buf[:limit]
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		}
	}
	waitErr := cmd.Wait()
	if over {
		return buf, &Error{Sub: sub, Code: -1, err: ErrOutputLimit}
	}
	if ctx.Err() != nil {
		return nil, &Error{Sub: sub, Code: -1, Line: firstLine(stderr.String()), err: fmt.Errorf("timed out: %w", ctx.Err())}
	}
	if waitErr != nil {
		code := -1
		if ee, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			code = ee.ExitCode()
		}
		return buf, &Error{Sub: sub, Code: code, Line: firstLine(stderr.String()), err: waitErr}
	}
	if readErr != nil {
		return nil, &Error{Sub: sub, Code: -1, err: readErr}
	}
	return buf, nil
}

// firstLine is the first non-empty line of s, cut to maxLine characters,
// with the userinfo of any URL in it removed and no control characters.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = scrubURLs(line) // before the cut, so no half a credential is left
		if utf8.RuneCountInString(line) > maxLine {
			line = string([]rune(line)[:maxLine])
		}
		// git relays a remote's sideband text: no escape sequence leaves.
		return oneLine(strings.ToValidUTF8(line, "?"))
	}
	return ""
}

// capped keeps the first max bytes written to it.
type capped struct {
	max int
	buf bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (c *capped) String() string { return c.buf.String() }

// exitCode is err's git exit code, or -1.
func exitCode(err error) int {
	if ge, ok := errors.AsType[*Error](err); ok {
		return ge.Code
	}
	return -1
}
