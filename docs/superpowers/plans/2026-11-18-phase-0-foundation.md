# Phase 0: Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `vaquero` binary that, in local mode, boots on loopback against a migrated SQLite file in the platform data directory, serves a themed placeholder home page and `/healthz`, shuts down cleanly, and passes the full gate.

**Architecture:** One Go binary with `serve`, `version` and `help` subcommands. SQLite with one writer connection and a read-only reader pool, goose migrations embedded and run before the listener binds, sqlc queries. chi router with server-rendered templ pages under a strict CSP, plus a Host-header check that stops other websites reaching the loopback server. Infrastructure is ported and trimmed from `../healthcheck-email`, following the cuts `../startcase` already made.

**Tech Stack:** Go 1.27, chi v5, templ v0.3.1020, htmx 2 (vendored), Tailwind v4 (build-time only), `modernc.org/sqlite`, goose v3, sqlc v1.31.1.

**Spec:** `docs/superpowers/specs/2026-11-18-vaquero-design.md`

**Porting reference:** `/Users/cstaszak/Stazelabs/projects/startcase/docs/superpowers/plans/2026-11-17-porting-map.md` (cited as "map §N"). Where this plan says "port", copy the named file from `/Users/cstaszak/Stazelabs/projects/startcase` (preferred, already trimmed) or, if startcase lacks it, from `/Users/cstaszak/Stazelabs/projects/healthcheck-email`, apply the cuts named here and in the cited map section, and port the matching tests with the same cuts. Both source projects are read-only.

## Global Constraints

- Module `github.com/stazelabs/vaquero`; binary `vaquero`; `go 1.27.0`, `toolchain go1.27.3`; always `CGO_ENABLED=0`.
- Env prefix `VQ_`.
- Dependency versions match startcase's `go.mod`. No ORM, no cgo SQLite, no React, no validation library, no testify, no cobra.
- Licence Apache 2.0. Supported platforms for the local binary: macOS and Linux.
- Phase 0 runs in local mode only. `VQ_MODE=hosted` is refused at boot until Phase 4 adds sign-in.
- Local mode binds loopback only and rejects any request whose Host is not `localhost`, `127.0.0.1` or `::1`.
- IDs come only from `internal/idgen` (ULID). Instants are stored as text only through `internal/timefmt`.
- Writer pool has exactly one connection. Inside a transaction use `gen.New(tx)`, never `Deps.Q`. Reads on request paths use the reader (`Deps.R`).
- CSP allows no inline script and no `style=` attribute. All behaviour is in `internal/web/static/js/app.js`.
- Templates use only the theme tokens defined in Task 4; no raw hex or oklch values in `.templ` files.
- Generated files (`*_templ.go`, `internal/db/gen`, `tailwind.css`) are committed; `make gen && git diff --exit-code` must be clean.
- Copy: the product is "Vaquero" in prose and `vaquero` as a command. Sentence case, no exclamation points. Documentation and UI never name competing products.
- No telemetry, no outbound network calls.
- Never add `push` or `pull_request` triggers to GitHub workflows.
- Commit messages: `{Area}: {summary}`, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Gate before every commit that touches Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`. Before generator inputs exist (Tasks 1–2) the gate is `make fmt vet lint test`.

## Review Focus

1. **Someone sets `VQ_ADDR=0.0.0.0:7878` or `:7878` in local mode** to reach vaquero from a phone. That would expose an unauthenticated tracker to the network, so boot must refuse with a message pointing to hosted mode. (Test in Task 2.)
2. **A page on another website reaches the loopback server through DNS rebinding** (the browser sends `Host: evil.example`). Every route, `/healthz` included, answers 403 and does nothing. (Test in Task 4.)
3. **The data directory sits under `~/Library/Application Support`**, a path with a space. The database must open and migrate there. (Test in Task 3.)
4. **A second `vaquero serve` starts while one is already running.** It exits 1 with a message naming the address in use, and leaves the running instance's database alone. (Test in Task 5.)
5. **A user rolls back to an older binary after a newer one migrated the database.** The old binary refuses to start with a clear message. (Ported test in Task 3.)

---

### Task 1: Repo scaffold, licence, version command

**Files:**
- Create: `go.mod`, `LICENSE`, `NOTICE`, `README.md`, `CLAUDE.md`, `.gitignore`, `Makefile`, `package.json`
- Create: `cmd/vaquero/main.go`, `cmd/vaquero/main_test.go`
- Create: `internal/version/version.go`, `internal/version/version_test.go`
- Port: `internal/idgen/`, `internal/timefmt/` with tests (map §11)

**Interfaces:**
- Produces: `func run(args []string, stdout, stderr io.Writer) int` in `cmd/vaquero/main.go`.
- Produces: in `internal/version`, `var Version = "0.0.0-dev"`, `var Commit = "dev"`, `var BuildDate = ""` (all stamped with `-ldflags -X`), and `func Line() string`.
- Produces: `idgen.New() string`; the `timefmt` API as in map §11.

- [ ] **Step 1: Commit the approved design and this plan** — `git add docs && git commit -m "Docs: design and Phase 0 plan"`
- [ ] **Step 2: Write the failing tests**

```go
// internal/version/version_test.go
func TestLine(t *testing.T) {
	Version, Commit, BuildDate = "1.2.3", "abc1234", ""
	if got := Line(); got != "vaquero 1.2.3 (abc1234)" { t.Fatalf("%q", got) }
	BuildDate = "2026-11-18T00:00:00Z"
	if got := Line(); got != "vaquero 1.2.3 (abc1234, 2026-11-18T00:00:00Z)" { t.Fatalf("%q", got) }
}

// cmd/vaquero/main_test.go
func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 { t.Fatalf("code %d: %s", code, errb.String()) }
	if !strings.HasPrefix(out.String(), "vaquero ") { t.Fatalf("%q", out.String()) }
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: vaquero") { t.Fatalf("%d %q", code, out.String()) }
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"nope"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage") { t.Fatalf("%d %q", code, errb.String()) }
}
```

- [ ] **Step 3:** `go mod init github.com/stazelabs/vaquero`; set `go 1.27.0` and `toolchain go1.27.3`. Run `go test ./...` — Expected: FAIL, `undefined: run` and `undefined: Line`.
- [ ] **Step 4:** Implement `version.Line` and `run` (shape of startcase `cmd/startcase/main.go`): commands `version`, `help`/`-h`/`--help`, no arguments (usage to stdout, 0), unknown (usage to stderr, 2). Usage lists `serve`, `version`, `help`. Port `idgen` and `timefmt` with their tests.
- [ ] **Step 5:** Write the `Makefile` from startcase's, with `BIN := bin/vaquero`, `PKG := github.com/stazelabs/vaquero`, build path `./cmd/vaquero`. Add `VERSION`: the newest `v*` tag without its `v` (`git describe --tags --match 'v*'`), else `0.0.0-dev`; stamp it into `internal/version.Version` alongside `Commit` and `BuildDate`. Drop `set-admin` and `signin-link`. `db-reset` deletes `./data` only. `dev` frees and uses port 7878. Keep the pinned `STATICCHECK_VERSION`, `LINT_TOOLCHAIN`, `TEMPL_VERSION` and `SQLC_VERSION` and their comment.
- [ ] **Step 6:** Write the remaining files:
  - `LICENSE`: the canonical Apache 2.0 text from `https://www.apache.org/licenses/LICENSE-2.0.txt`.
  - `NOTICE`: `Vaquero` and `Copyright 2026 Stazelabs`.
  - `.gitignore` from startcase's, with the binary name changed.
  - `package.json` with only `@tailwindcss/cli` and `tailwindcss` `^4.1.0`.
  - `README.md`: the title, the tagline "Vaquero herds the chaos.", one paragraph on what it is (from the spec's Intent), a status line ("Early development. Not ready for use."), and build-from-source steps (`make tools`, `make dev`).
  - `CLAUDE.md` with the sections of startcase's: Summary (link the spec and this plan), Commands, Package map, Hard invariants (copy the Global Constraints above), Rejected (mattn/go-sqlite3, ORMs, gin/echo/fiber, React/Vite, Redis, validation libraries, testify, cobra, telemetry, Docker as a runtime requirement for local use). Add one line under Hard invariants: "Departures from the siblings are deliberate: `internal/core` owns every mutation (Phase 1); passwords and passkeys (Phase 4); the CLI is an HTTP client (Phase 2)."
- [ ] **Step 7:** Run `go test ./... && make vet` — Expected: PASS.
- [ ] **Step 8: Commit** — `"Scaffold: module, licence, Makefile, version command"`

---

### Task 2: Config with modes and data directory

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:

```go
type Mode string
const (
	ModeLocal  Mode = "local"
	ModeHosted Mode = "hosted"
)
type Config struct {
	Mode      Mode
	Addr      string // default "127.0.0.1:7878"
	DataDir   string // VQ_DATA_DIR, else DefaultDataDir
	DBPath    string // always filepath.Join(DataDir, "vaquero.db")
	LogFormat string // "text" (default) or "json"
	LogLevel  string // default "info"
}
func Load() (Config, error) // best-effort godotenv.Load(".env.local"), then FromEnv(os.Getenv)
func FromEnv(get func(string) string) (Config, error)
func DefaultDataDir(goos string, get func(string) string) (string, error)
func (c Config) AllowedHosts() []string // local mode: "localhost", "127.0.0.1", "::1"
```

Variables: `VQ_MODE`, `VQ_ADDR`, `VQ_DATA_DIR`, `VQ_LOG_FORMAT`, `VQ_LOG_LEVEL`. `DefaultDataDir`: on `darwin`, `$HOME/Library/Application Support/vaquero`; otherwise `$XDG_DATA_HOME/vaquero` when set, else `$HOME/.local/share/vaquero`; an error when `HOME` is empty.

- [ ] **Step 1: Write the failing tests**

```go
func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLocalDefaults(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"HOME": "/home/u"}))
	if err != nil { t.Fatal(err) }
	if c.Mode != ModeLocal || c.Addr != "127.0.0.1:7878" || c.LogFormat != "text" || c.LogLevel != "info" { t.Fatalf("%+v", c) }
	if c.DBPath != filepath.Join(c.DataDir, "vaquero.db") { t.Fatalf("%q", c.DBPath) }
}

func TestDefaultDataDir(t *testing.T) {
	for _, tc := range []struct{ goos string; env map[string]string; want string }{
		{"darwin", map[string]string{"HOME": "/Users/u"}, "/Users/u/Library/Application Support/vaquero"},
		{"linux", map[string]string{"HOME": "/home/u"}, "/home/u/.local/share/vaquero"},
		{"linux", map[string]string{"HOME": "/home/u", "XDG_DATA_HOME": "/x"}, "/x/vaquero"},
	} {
		got, err := DefaultDataDir(tc.goos, env(tc.env))
		if err != nil || got != tc.want { t.Fatalf("%s: %q %v", tc.goos, got, err) }
	}
	if _, err := DefaultDataDir("linux", env(nil)); err == nil { t.Fatal("want error with no HOME") }
}

func TestDataDirOverride(t *testing.T) {
	c, err := FromEnv(env(map[string]string{"VQ_DATA_DIR": "/tmp/vq"}))
	if err != nil || c.DBPath != "/tmp/vq/vaquero.db" { t.Fatalf("%q %v", c.DBPath, err) }
}

// Review Focus 1
func TestLocalRefusesNonLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:7878", ":7878", "192.168.1.5:7878", "example.com:7878"} {
		_, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_ADDR": addr}))
		if err == nil || !strings.Contains(err.Error(), "loopback") { t.Fatalf("%s: %v", addr, err) }
	}
	for _, addr := range []string{"127.0.0.1:9000", "localhost:9000", "[::1]:9000"} {
		if _, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_ADDR": addr})); err != nil { t.Fatalf("%s: %v", addr, err) }
	}
}

func TestHostedNotYetAvailable(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"HOME": "/h", "VQ_MODE": "hosted"}))
	if err == nil || !strings.Contains(err.Error(), "not available yet") { t.Fatalf("%v", err) }
}

func TestBadValuesRejected(t *testing.T) {
	for k, v := range map[string]string{"VQ_MODE": "cloud", "VQ_LOG_FORMAT": "xml", "VQ_ADDR": "nope"} {
		if _, err := FromEnv(env(map[string]string{"HOME": "/h", k: v})); err == nil { t.Fatalf("%s=%s accepted", k, v) }
	}
}
```

- [ ] **Step 2:** Run `go test ./internal/config` — Expected: FAIL, no Go files.
- [ ] **Step 3:** Implement following the shape of startcase's `config.go`: one flat struct, no struct tags, errors joined so every problem is reported at once. `FromEnv` calls `DefaultDataDir(runtime.GOOS, get)` only when `VQ_DATA_DIR` is empty. The non-loopback error reads: ``VQ_ADDR must be a loopback address (127.0.0.1, localhost or [::1]) in local mode; sharing on a network needs hosted mode``. The hosted error reads: `VQ_MODE=hosted is not available yet; this build runs in local mode only`.
- [ ] **Step 4:** Run `go test ./internal/config` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Config: VQ_* loading, local mode, data directory"`

---

### Task 3: Database, first migration, test databases

**Files:**
- Port: `internal/db/sqlite.go`, `migrate.go`, `sqlc.yaml`, `dbtest/dbtest.go`, and tests `db_test.go`, `txlock_test.go`, `guard_test.go` (map §2)
- Create: `internal/db/migrations/0001_init.sql`, `internal/db/queries/settings.sql`
- Generated: `internal/db/gen/`

**Interfaces:**
- Produces: `db.Open(path string) (*sql.DB, error)`, `db.OpenReader(path string, n int) (*sql.DB, error)`, `db.Checkpoint(*sql.DB) error`, `db.Migrate(*sql.DB) error`, `db.Newest() (int64, error)`, `db.Version(*sql.DB) (int64, error)`, `db.MigrateTo(*sql.DB, int64) error`.
- Produces: `dbtest.New(t testing.TB) dbtest.DB` with fields `W, R *sql.DB; Q, RQ *gen.Queries`.
- Produces: table `settings (key TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 100), value TEXT NOT NULL, updated_at TEXT NOT NULL)`; gen methods `GetSetting(ctx, key string) (gen.Setting, error)` (`:one`) and `PutSetting(ctx, gen.PutSettingParams{Key, Value, UpdatedAt string}) error` (`:exec`, `INSERT … ON CONFLICT(key) DO UPDATE`).

- [ ] **Step 1: Write the failing tests** in `internal/db/db_test.go` (alongside the ported ones)

```go
func TestMigrateIsIdempotent(t *testing.T) {
	d := dbtest.New(t)
	if err := appdb.Migrate(d.W); err != nil { t.Fatal(err) }
	v, _ := appdb.Version(d.W); n, _ := appdb.Newest()
	if v != n { t.Fatalf("version %d, newest %d", v, n) }
}

func TestSettingsRoundTrip(t *testing.T) {
	d := dbtest.New(t); ctx := context.Background()
	for _, val := range []string{"a", "b"} {
		if err := d.Q.PutSetting(ctx, gen.PutSettingParams{Key: "k", Value: val, UpdatedAt: "2026-11-18T00:00:00Z"}); err != nil { t.Fatal(err) }
	}
	s, err := d.RQ.GetSetting(ctx, "k")
	if err != nil || s.Value != "b" { t.Fatalf("%+v %v", s, err) }
}

func TestReaderRefusesWrites(t *testing.T) {
	d := dbtest.New(t)
	if _, err := d.R.Exec(`DELETE FROM settings`); err == nil { t.Fatal("reader accepted a write") }
}

// Review Focus 3
func TestOpenPathWithSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Application Support", "vaquero", "vaquero.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { t.Fatal(err) }
	w, err := appdb.Open(path)
	if err != nil { t.Fatal(err) }
	defer w.Close()
	if err := appdb.Migrate(w); err != nil { t.Fatal(err) }
	if _, err := os.Stat(path); err != nil { t.Fatalf("database not at %q: %v", path, err) }
}

// Review Focus 5
func TestMigrateRefusesNewerDatabase(t *testing.T) {
	d := dbtest.New(t)
	if _, err := d.W.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, 1)`); err != nil { t.Fatal(err) }
	if err := appdb.Migrate(d.W); err == nil || !strings.Contains(err.Error(), "newer") { t.Fatalf("err = %v", err) }
}
```

- [ ] **Step 2:** Run `go test ./internal/db/...` — Expected: FAIL to compile.
- [ ] **Step 3:** Port `sqlite.go` as startcase has it. If `TestOpenPathWithSpaces` fails, build the DSN with `url.URL{Scheme: "file", Path: path}` instead of string concatenation. Port `migrate.go` keeping only `Migrate`, `Version`, `MigrateTo`, `Newest` and `migrationFiles`; change the ahead-check message to `the database was written by a newer vaquero (migration %d; this build knows up to %d); install that version or newer`. Port `dbtest.go` with temp-dir prefix `vaquero-db-template-`.
- [ ] **Step 4:** Write `0001_init.sql` (`-- +goose Up` creates `settings`; `-- +goose Down` `SELECT 1;`), `queries/settings.sql`, and `sqlc.yaml` from map §2 with no `overrides`. Run `make sqlc`.
- [ ] **Step 5:** Run `go test ./internal/db/...` — Expected: PASS.
- [ ] **Step 6: Commit** — `"DB: pools, embedded migrations, settings table"`

---

### Task 4: Web shell, theme, middleware and test harness

**Files:**
- Port: `internal/web/middleware/requestid.go`, `recover.go`, `logger.go`, `security.go`, `csp.go`, `crossorigin.go`, `bodylimit.go`, `path.go` with tests (map §7)
- Create: `internal/web/middleware/hostcheck.go`, `hostcheck_test.go`
- Create: `internal/web/router.go`, `internal/web/static.go`, `internal/web/router_test.go`
- Create: `internal/web/handlers/deps.go`, `render.go`, `misc.go`, `home.go`
- Create: `internal/web/templates/layout.templ`, `home.templ`, `errors.templ`, `helpers.go`
- Create: `internal/web/static/static.go`, `css/input.css`, `css/tailwind.css` (generated), `js/app.js`, `vendor/htmx.min.js` (copy from startcase), `favicon.svg`
- Create: `internal/web/webtest/webtest.go`

**Interfaces:**
- Produces:

```go
package middleware
func HostCheck(allowed []string) func(http.Handler) http.Handler // 403 "unknown host" when r.Host (port stripped, brackets removed) is not in allowed

package handlers
type Deps struct {
	Cfg    config.Config
	DB     *sql.DB      // writer pool, for transactions and /healthz
	Q      *gen.Queries // writer
	R      *gen.Queries // reader
	Logger *slog.Logger
	Now    func() time.Time
}
func render(w http.ResponseWriter, r *http.Request, d Deps, status int, c templ.Component)
func serverError(w http.ResponseWriter, r *http.Request, d Deps, err error)
func Healthz(d Deps) http.HandlerFunc
func NotFound(d Deps) http.HandlerFunc
func MethodNotAllowed(d Deps) http.HandlerFunc
func Home(d Deps) http.HandlerFunc

package web
func NewRouter(d handlers.Deps) http.Handler

package templates
templ Shell(title string) // <head>, top bar with the wordmark, <main id="main">
templ HomePage()
templ NotFoundPage()
templ ErrorPage()

package static
var FS embed.FS
func URL(path string) string

package webtest
type Env struct { DB dbtest.DB; Deps handlers.Deps; Server *httptest.Server }
func New(t testing.TB, opts ...func(*config.Config)) *Env
func (e *Env) Client() *Client
type Response struct { Status int; Location string; Header http.Header; Body string }
func (c *Client) Get(path string) *Response
func (c *Client) GetWithHost(host, path string) *Response
func (c *Client) Post(path string, form url.Values) *Response
func (c *Client) PostCrossSite(path string, form url.Values) *Response
```

- Global middleware order: `RequestID`, `Recover`, `HostCheck(d.Cfg.AllowedHosts())`, `Logger`, `SecurityHeaders`, `CSP`, `CrossOrigin`, `BodyLimit(MaxBody)`. Routes: `GET /healthz`, `GET /static/*`, `GET /`, NotFound, MethodNotAllowed.
- `webtest.New` builds config with `FromEnv` from `VQ_DATA_DIR=t.TempDir()`, uses `dbtest.New`, a fixed clock at `2026-11-18T12:00:00Z`, and an `httptest.Server` whose requests carry `Host: 127.0.0.1`.

**Theme tokens** (`input.css` `@theme`, structure as map §9; my call, adjustable once the brand is designed):

```css
--color-ink:       oklch(0.24 0.02 50);   /* body text */
--color-paper:     oklch(0.985 0.008 85); /* page background */
--color-paper-alt: oklch(0.955 0.012 80); /* cards, columns */
--color-line:      oklch(0.88 0.015 75);  /* borders */
--color-muted:     oklch(0.55 0.02 60);   /* secondary text */
--color-accent:    oklch(0.55 0.13 45);   /* saddle rust: links, primary buttons */
--color-danger:    oklch(0.52 0.19 27);
--color-warn:      oklch(0.62 0.13 75);
--font-sans: ui-sans-serif, system-ui, sans-serif;
--font-mono: ui-monospace, SFMono-Regular, Menlo, monospace;
```

Dark mode under `@media (prefers-color-scheme: dark)`: ink `oklch(0.93 0.01 80)`, paper `oklch(0.19 0.01 60)`, paper-alt `oklch(0.23 0.012 60)`, line `oklch(0.33 0.015 60)`, muted `oklch(0.68 0.02 70)`, accent `oklch(0.72 0.12 50)`, danger `oklch(0.68 0.17 27)`, warn `oklch(0.78 0.12 80)`. System fonts only, so no font files are downloaded.

- [ ] **Step 1: Write the failing tests** in `internal/web/router_test.go`

```go
func TestHealthz(t *testing.T) {
	r := webtest.New(t).Client().Get("/healthz")
	if r.Status != 200 || r.Body != "ok" { t.Fatalf("%d %q", r.Status, r.Body) }
}

func TestHomeIsThemed(t *testing.T) {
	r := webtest.New(t).Client().Get("/")
	if r.Status != 200 || !strings.Contains(r.Body, "Vaquero") || !strings.Contains(r.Body, "herds the chaos") { t.Fatalf("%d", r.Status) }
}

func TestSecurityHeaders(t *testing.T) {
	r := webtest.New(t).Client().Get("/")
	csp := r.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) { t.Fatalf("CSP missing %q: %s", want, csp) }
	}
}

func TestStaticAssets(t *testing.T) {
	e := webtest.New(t)
	for _, p := range []string{"css/tailwind.css", "vendor/htmx.min.js", "js/app.js", "favicon.svg"} {
		if r := e.Client().Get(static.URL(p)); r.Status != 200 { t.Fatalf("%s: %d", p, r.Status) }
	}
}

func TestNotFoundIsThemed(t *testing.T) {
	r := webtest.New(t).Client().Get("/no-such-page")
	if r.Status != 404 || !strings.Contains(r.Body, "Vaquero") { t.Fatalf("%d", r.Status) }
}

// Review Focus 2
func TestForeignHostRefused(t *testing.T) {
	e := webtest.New(t)
	for _, p := range []string{"/", "/healthz", static.URL("js/app.js")} {
		if r := e.Client().GetWithHost("evil.example", p); r.Status != 403 { t.Fatalf("%s: %d", p, r.Status) }
	}
	for _, h := range []string{"localhost:7878", "127.0.0.1:7878", "[::1]:7878"} {
		if r := e.Client().GetWithHost(h, "/healthz"); r.Status != 200 { t.Fatalf("%s: %d", h, r.Status) }
	}
}

func TestCrossSitePostRefused(t *testing.T) {
	if r := webtest.New(t).Client().PostCrossSite("/", nil); r.Status != 403 { t.Fatalf("%d", r.Status) }
}

func TestNoRawColoursInTemplates(t *testing.T) {
	re := regexp.MustCompile(`#[0-9a-fA-F]{6}\b|oklch\(`)
	files, _ := filepath.Glob("templates/*.templ")
	if len(files) == 0 { t.Fatal("no templates found") }
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if m := re.Find(b); m != nil { t.Fatalf("%s uses raw colour %s", f, m) }
	}
}
```

- [ ] **Step 2:** Run `go test ./internal/web/...` — Expected: FAIL to compile.
- [ ] **Step 3:** Port the middleware files with the cuts in map §7: no `APIRecover`, no session or auth middleware, no rate limiters, no robots or client-IP handling; `redactPath` hides only the query string; `CrossOrigin` has no bypass patterns; `BodyLimit` has only `MaxBody`. Write `HostCheck` with a table test in `hostcheck_test.go` covering port stripping, bracketed IPv6 and case-insensitive `LOCALHOST`.
- [ ] **Step 4:** Write `static.go` (embeds `css js vendor favicon.svg`; `URL` appends `?v=` plus the first 10 hex digits of the file's SHA-256), `input.css` with the tokens above, `favicon.svg` (a lasso loop in the accent colour, inline fill values allowed here only), and `app.js` (empty module with a header comment saying all page behaviour goes here). Run `make css`.
- [ ] **Step 5:** Write the templates and `helpers.go` (`htmxConfig` exactly as map §9). `Shell` follows map §9's `<head>` with title suffix " · Vaquero", no social card or manifest; body class `min-h-screen bg-paper text-ink font-sans antialiased`. `HomePage` shows the wordmark, the line "Vaquero herds the chaos." and "Nothing to herd yet. Projects and tickets arrive in the next phase."
- [ ] **Step 6:** Write `deps.go`, `render.go`, `misc.go` (port `Healthz`, `NotFound`, `MethodNotAllowed` from startcase `internal/web/handlers/misc.go`), `home.go` and `router.go` with the middleware order and routes in Interfaces.
- [ ] **Step 7:** Write `webtest` per map §8 "Trimmed equivalent", reduced to the Interfaces above; `GetWithHost` sets `req.Host`.
- [ ] **Step 8:** Run `make gen && go test ./internal/web/...` — Expected: PASS.
- [ ] **Step 9: Commit** — `"Web: themed shell, middleware, host check, test harness"`

---

### Task 5: `serve` wiring and shutdown

**Files:**
- Create: `cmd/vaquero/serve.go`, `cmd/vaquero/serve_test.go`
- Modify: `cmd/vaquero/main.go` (dispatch `serve`; add `openDB` and `newLogger` as in startcase's `main.go`)

**Interfaces:**
- Consumes: `config.Load`, `db.Open`/`OpenReader`/`Migrate`/`Checkpoint`, `web.NewRouter`, `handlers.Deps`.
- Produces: `func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error`; `func openDB(path string) (w, r *sql.DB, err error)`; `func newLogger(cfg config.Config, out io.Writer) *slog.Logger`.

- [ ] **Step 1: Write the failing tests** in `cmd/vaquero/serve_test.go`

```go
func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0"); if err != nil { t.Fatal(err) }
	defer ln.Close(); return ln.Addr().String()
}

func testConfig(t *testing.T, addr, dir string) config.Config {
	cfg, err := config.FromEnv(func(k string) string { return map[string]string{"VQ_ADDR": addr, "VQ_DATA_DIR": dir}[k] })
	if err != nil { t.Fatal(err) }
	return cfg
}

func TestServeStartsAndStopsCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "vaquero") // does not exist yet
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, dir), slog.New(slog.DiscardHandler)) }()
	var resp *http.Response; var err error
	for range 50 {
		if resp, err = http.Get("http://" + addr + "/healthz"); err == nil { break }
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil { t.Fatal(err) }
	if b, _ := io.ReadAll(resp.Body); string(b) != "ok" { t.Fatalf("%q", b) }
	cancel()
	select {
	case err := <-done: if err != nil { t.Fatal(err) }
	case <-time.After(10 * time.Second): t.Fatal("serve did not stop")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 { t.Fatalf("data dir: %v %v", fi, err) }
	if fi, err := os.Stat(filepath.Join(dir, "vaquero.db-wal")); err == nil && fi.Size() != 0 { t.Fatal("WAL not checkpointed") }
}

// Review Focus 4
func TestServeReportsAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0"); if err != nil { t.Fatal(err) }
	defer ln.Close()
	addr := ln.Addr().String()
	err = serve(context.Background(), testConfig(t, addr, t.TempDir()), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), addr) || !strings.Contains(err.Error(), "already running") { t.Fatalf("%v", err) }
}
```

- [ ] **Step 2:** Run `go test ./cmd/vaquero -run Serve` — Expected: FAIL, `undefined: serve`.
- [ ] **Step 3:** Implement `serve`, following map §6 `serve.go` without Sentry, sessions, mail, jobs or sweeper:
  1. `os.MkdirAll(cfg.DataDir, 0o700)`, then `os.Chmod` to 0o700 in case it already existed looser.
  2. `openDB(cfg.DBPath)`.
  3. `net.Listen("tcp", cfg.Addr)` before starting anything else. On failure, close the database and return `cannot listen on %s: %w (is vaquero already running?)`.
  4. Build `handlers.Deps` and `web.NewRouter`; run `http.Server` with startcase's timeouts.
  5. Log `vaquero is running` with `url=http://<addr>`, `data=<DataDir>` and `version=<version.Version>`.
  6. On `ctx.Done()`: `Shutdown` with `drainTimeout = 30 * time.Second`, then `db.Checkpoint(w)`, then close both pools.
- [ ] **Step 4:** In `run`, the `serve` case calls `config.Load`, wraps `context.Background()` with `signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)`, and returns 1 with the error on stderr on failure.
- [ ] **Step 5:** Run `go test ./cmd/vaquero` — Expected: PASS.
- [ ] **Step 6:** Manual check: run `make dev` and open `http://127.0.0.1:7878`. Expected: the themed home page in light and in dark mode, and no CSP errors in the browser console. Then press Ctrl-C. Expected: a clean exit.
- [ ] **Step 7: Commit** — `"Serve: local server wiring and graceful shutdown"`

---

### Task 6: Dev loop, agent rules, CI and coverage floors

**Files:**
- Create: `.air.toml`, `.env.local.example`, `scripts/cover-check.sh`, `coverage-floors.txt`
- Create: `.github/workflows/quality-gate.yml`, `.github/workflows/ci.yml`
- Create: `.claude/rules/database.md`, `handlers.md`, `security.md`, `ui-theming.md`, `copy-voice.md`
- Modify: `CLAUDE.md` (package map filled in), `README.md` (dev commands)

**Interfaces:**
- Consumes: every Make target above.

- [ ] **Step 1:** Port `.air.toml` (binary `vaquero`, pre_cmd `make templ` and `make css`, the run command `serve`). Write `.env.local.example` with `VQ_DATA_DIR=./data`, `VQ_ADDR=127.0.0.1:7878`, `VQ_LOG_LEVEL=debug`, and add `.env.local` to `.gitignore` if it is not already there.
- [ ] **Step 2:** Port `scripts/cover-check.sh` and the `coverage-floors.txt` header from map §10. Run `make cover` and set each floor 5 points below its measured value, rounded down to a multiple of 5, for `cmd/vaquero`, `internal/config`, `internal/db`, `internal/version`, `internal/idgen`, `internal/timefmt`, `internal/web`, `internal/web/middleware` and `internal/web/handlers`.
- [ ] **Step 3:** Port `quality-gate.yml` and `ci.yml` from startcase unchanged apart from names. `ci.yml` stays `workflow_dispatch` only, with its "never add push or pull_request triggers" comment.
- [ ] **Step 4:** Port the five `.claude/rules` files from healthcheck-email (map §12), keeping their `paths:` frontmatter. Make these changes:
  - Remove sessions, roles, workspaces, magic links, email and LLM content.
  - Point at vaquero's own packages.
  - `security.md` adds: local mode binds loopback only, and `HostCheck` stays first after `Recover`.
  - `copy-voice.md` adds: "Vaquero" in prose, `vaquero` for the command, and never name competing products.
  - `handlers.md` adds: once `internal/core` exists (Phase 1), handlers must not call writer queries directly.
- [ ] **Step 5:** Complete the `CLAUDE.md` package map for every package that now exists. Add `make dev`, `make test` and `make ci` to the README.
- [ ] **Step 6:** Run `make ci && git status --porcelain` — Expected: `make ci` passes, and the status shows only the files from this task (no regenerated files changed).
- [ ] **Step 7: Commit** — `"Tooling: dev loop, agent rules, CI gate, coverage floors"`
- [ ] **Step 8: STOP.** Do not create a GitHub repository, push, or tag. Report that Phase 0 is complete and ask the owner whether to create the public `stazelabs/vaquero` repository, and whether vaquero's own tickets should be tracked in the current tracker (via `/stazelabs:setup`) until Phase 3 moves them into vaquero.
