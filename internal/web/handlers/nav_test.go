package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/handlers"
	"github.com/stazelabs/vaquero/internal/web/templates"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The sidebar's place comes from the path (ruling T-R2); a ticket page
// names its project once it has read the ticket.
func TestNavPlaceFromPath(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "one"})
	for path, want := range map[string]templates.Place{
		"/p/vaq":          {Project: "VAQ", View: "focus"},
		"/p/VAQ/":         {Project: "VAQ", View: "focus"},
		"/p/vaq/board":    {Project: "VAQ", View: "board"},
		"/p/VAQ/list":     {Project: "VAQ", View: "list"},
		"/p/VAQ/settings": {Project: "VAQ", View: "settings"},
		"/p/VAQ/new":      {Project: "VAQ"},
		"/t/VAQ-1":        {View: "ticket"},
		"/inbox":          {View: "inbox"},
		"/my":             {View: "my"},
		"/activity":       {View: "activity"},
		"/search":         {View: "search"},
		"/help":           {View: "help"},
		"/help/cli":       {View: "help"},
		"/profile":        {View: "profile"},
		"/":               {},
		"/admin":          {},
	} {
		var got templates.Place
		h := handlers.Nav(e.Deps)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = templates.PlaceOf(r.Context())
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if got != want {
			t.Errorf("%s: place %+v, want %+v", path, got, want)
		}
	}

	// After the ticket page renders, the place names the ticket's project.
	var before, after templates.Place
	r := chi.NewRouter()
	r.Use(handlers.Faces(e.Deps), handlers.Nav(e.Deps))
	r.Get("/t/{ticket}", func(w http.ResponseWriter, req *http.Request) {
		before = templates.PlaceOf(req.Context())
		handlers.TicketShow(e.Deps)(w, req)
		after = templates.PlaceOf(req.Context())
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t/VAQ-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket page %d", rec.Code)
	}
	if before != (templates.Place{View: "ticket"}) || after != (templates.Place{Project: "VAQ", View: "ticket"}) {
		t.Fatalf("ticket place before %+v, after %+v", before, after)
	}
	// And its sidebar marks the project, Focus marked until app.js reads
	// the last view.
	side := sidebar(t, rec.Body.String())
	contains(t, side, `data-nav-project="VAQ" aria-current="page"`, `data-last-view`)
	if v := markedViews(side); len(v) != 1 || v[0] != "focus" {
		t.Fatalf("marked views %v, want focus", v)
	}
}

// sidebar is the page's sidebar markup.
func sidebar(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, `<nav id="vq-sidebar"`)
	if i < 0 {
		t.Fatal("no sidebar")
	}
	j := strings.Index(page[i:], "</nav>")
	if j < 0 {
		t.Fatal("sidebar never ends")
	}
	return page[i : i+j]
}

var (
	navProjectRe = regexp.MustCompile(`data-nav-project="([^"]+)"`)
	navViewRe    = regexp.MustCompile(`<a [^>]*data-nav-view="([a-z]+)"[^>]*>`)
	// currentAttr is the data-current attribute itself, not the classes
	// that style it.
	currentAttr = regexp.MustCompile(`\sdata-current[\s>]`)
)

// markedViews are the project views the sidebar marks current.
func markedViews(side string) []string {
	var out []string
	for _, m := range navViewRe.FindAllStringSubmatch(side, -1) {
		if currentAttr.MatchString(m[0]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Two projects and an archived one: two rows, the archived one absent; the
// current project is marked and shows its three views, the current one
// marked.
func TestSidebarListsProjects(t *testing.T) {
	e := webtest.New(t)
	for _, k := range []string{"BBB", "AAA", "OLD"} {
		p, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: k, Name: k + " project"})
		if err != nil {
			t.Fatal(err)
		}
		if k == "OLD" {
			if err := e.Deps.Core.SetProjectArchived(bg, e.Deps.Actor.ID, p.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	side := sidebar(t, e.Client().Get("/p/AAA/board").Body)
	var keys []string
	for _, m := range navProjectRe.FindAllStringSubmatch(side, -1) {
		keys = append(keys, m[1])
	}
	if strings.Join(keys, " ") != "AAA BBB" {
		t.Fatalf("sidebar projects %v, want AAA BBB by name", keys)
	}
	lacks(t, side, "OLD project", `data-nav-project="BBB" aria-current`)
	contains(t, side, `data-nav-project="AAA" aria-current="page"`,
		`href="/p/AAA" data-nav-view="focus"`, `href="/p/AAA/board" data-nav-view="board"`, `href="/p/AAA/list" data-nav-view="list"`,
		`href="/"`, "All projects", `id="vq-search"`, `placeholder="Search"`, "⌘K",
		`href="/inbox"`, `href="/my"`, `href="/activity"`, `href="/profile"`, `href="/help"`, `href="/admin"`, "data-shortcuts-open")
	if v := markedViews(side); len(v) != 1 || v[0] != "board" {
		t.Fatalf("marked views %v, want board", v)
	}
	// Only the current project expands.
	if strings.Contains(side, `href="/p/BBB/board"`) {
		t.Fatal("BBB, not current, shows its views")
	}
	// Activity is marked on its page, and no project is.
	side = sidebar(t, e.Client().Get("/activity").Body)
	contains(t, side, `href="/activity" data-current aria-current="page"`)
	lacks(t, side, `data-nav-view=`)
}

// navCountingDB counts the queries named name run through it, and fails them
// when fail is set.
type navCountingDB struct {
	gen.DBTX
	name string
	fail bool
	mu   sync.Mutex
	n    int
}

func (c *navCountingDB) match(q string) bool {
	return strings.HasPrefix(q, "-- name: "+c.name+" ")
}

func (c *navCountingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if c.match(q) {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
		if c.fail {
			return nil, errors.New("the nav read failed")
		}
	}
	return c.DBTX.QueryContext(ctx, q, args...)
}

func (c *navCountingDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if c.match(q) {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
		if c.fail {
			done, cancel := context.WithCancel(ctx)
			cancel() // a row that fails as a failed read would
			return c.DBTX.QueryRowContext(done, q, args...)
		}
	}
	return c.DBTX.QueryRowContext(ctx, q, args...)
}

func (c *navCountingDB) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// The sidebar reads its projects once per page, and not at all for a
// request that draws no page.
func TestSidebarOneQuery(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	for range 3 {
		ticket(t, e, p, core.TicketInput{Title: "t"})
	}
	cdb := &navCountingDB{DBTX: e.DB.R, name: "ListNavProjects"}
	e.Deps.R = gen.New(cdb)
	e.Rebuild()
	c := e.Client()
	for _, path := range []string{"/p/VAQ", "/p/VAQ/board", "/t/VAQ-1", "/activity", "/my"} {
		before := cdb.count()
		if r := c.Get(path); r.Status != http.StatusOK {
			t.Fatalf("%s: %d", path, r.Status)
		}
		if n := cdb.count() - before; n != 1 {
			t.Errorf("%s read the sidebar's projects %d times, want 1", path, n)
		}
	}
	before := cdb.count()
	c.Get("/p/VAQ/board/fragment")
	c.API(http.MethodGet, "/api/v1/projects", e.Token(t), nil)
	if n := cdb.count() - before; n != 0 {
		t.Errorf("a fragment and an API call read the sidebar's projects %d times", n)
	}
}

// A failed read draws the page with no projects in the sidebar, and logs
// once.
func TestSidebarLoadFailure(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	var buf bytes.Buffer
	var mu sync.Mutex
	e.Deps.Logger = slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, nil))
	e.Deps.R = gen.New(&navCountingDB{DBTX: e.DB.R, name: "ListNavProjects", fail: true})
	e.Rebuild()
	r := e.Client().Get("/p/VAQ/board")
	if r.Status != http.StatusOK {
		t.Fatalf("board %d with a failed sidebar read", r.Status)
	}
	side := sidebar(t, r.Body)
	lacks(t, side, "data-nav-project=")
	contains(t, side, "All projects", `href="/inbox"`)
	contains(t, r.Body, `id="board"`)
	mu.Lock()
	logged := strings.Count(buf.String(), "read sidebar")
	mu.Unlock()
	if logged != 1 {
		t.Fatalf("logged the failure %d times, want 1:\n%s", logged, buf.String())
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}
