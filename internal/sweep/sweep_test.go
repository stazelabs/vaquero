package sweep_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/sweep"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

var bg = context.Background()

func TestSweepExpires(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	owner, _ := c.EnsureOwner(bg)
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "x"})
	a, _ := c.EnsureAgent(bg, owner.ID, "builder")
	if _, err := c.Claim(bg, a.ID, tk.ID, 0); err != nil {
		t.Fatal(err)
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock}
	if n, err := s.Tick(bg); n != 0 || err != nil {
		t.Fatalf("early: %d %v", n, err)
	}
	now = now.Add(16 * time.Minute)
	if n, err := s.Tick(bg); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if got, _ := d.RQ.GetTicket(bg, tk.ID); got.ClaimedBy.Valid || got.AssigneeID.Valid {
		t.Fatalf("%+v", got)
	}
}

func TestSweeperStartStop(t *testing.T) {
	d := dbtest.New(t)
	c := core.New(d.W, time.Now)
	ticks := make(chan struct{}, 10)
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: time.Now, Interval: 5 * time.Millisecond,
		OnTick: func() { ticks <- struct{}{} }}
	s.Start()
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("never ticked")
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung")
	}
	s.Stop() // a second Stop is harmless
}

func TestMCPLogPruned(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	c := core.New(d.W, func() time.Time { return now })
	q := gen.New(d.W)
	for id, at := range map[string]time.Time{"01OLD": now.Add(-31 * 24 * time.Hour), "01NEW": now.Add(-29 * 24 * time.Hour)} {
		if err := q.RecordMCPCall(bg, gen.RecordMCPCallParams{ID: id, At: timefmt.Format(at), Tool: "search", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	rows, _ := d.RQ.ListMCPCalls(bg, gen.ListMCPCallsParams{Lim: 10})
	if len(rows) != 1 || rows[0].ID != "01NEW" {
		t.Fatalf("%+v", rows)
	}
}

// listSpy records the prefixes the sweep lists, and fails deletes of
// failKey.
type listSpy struct {
	blobs.Store
	prefixes []string
	failKey  string
	failList bool
}

func (s *listSpy) Delete(ctx context.Context, key string) error {
	if key == s.failKey {
		return errors.New("disk on fire")
	}
	return s.Store.Delete(ctx, key)
}

func (s *listSpy) List(ctx context.Context, prefix string, fn func(string, time.Time) error) error {
	s.prefixes = append(s.prefixes, prefix)
	if s.failList {
		return errors.New("listing failed")
	}
	return s.Store.List(ctx, prefix, fn)
}

func TestSweepDeletesOldOrphans(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	owner, _ := c.EnsureOwner(bg)
	p, _ := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	tk, _ := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "x"})
	store := &listSpy{Store: blobs.NewSQLite(d.W, d.R, clock)}
	put := func(key string, age time.Duration) {
		t.Helper()
		was := now
		now = now.Add(-age)
		defer func() { now = was }()
		if err := store.Put(bg, key, strings.NewReader("abc"), 3, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	put("attachments/inst/old", 2*time.Hour)
	put("attachments/inst/fresh", 10*time.Minute)
	put("attachments/inst/ref", 2*time.Hour)
	put("attachments/other/old", 2*time.Hour)
	if _, err := c.AddAttachment(bg, owner.ID, core.AttachmentInput{TicketID: tk.ID, Name: "ref.txt", ContentType: "text/plain", Size: 3, BlobKey: "attachments/inst/ref"}); err != nil {
		t.Fatal(err)
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock, Blobs: store, Instance: "inst"}
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	var left []string
	_ = store.Store.List(bg, "", func(k string, _ time.Time) error { left = append(left, k); return nil })
	if strings.Join(left, " ") != "attachments/inst/fresh attachments/inst/ref attachments/other/old" {
		t.Fatalf("left %v", left)
	}
	if strings.Join(store.prefixes, " ") != "attachments/inst/ thumbs/inst/" {
		t.Fatalf("listed %v", store.prefixes)
	}
	// A sweeper without a store skips the pass.
	if _, err := (&sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock}).Tick(bg); err != nil {
		t.Fatal(err)
	}
}

// One failed delete doesn't stop the pass, and the pass runs at most once
// per OrphanAge.
func TestSweepOrphanPassSurvivesFailuresAndRunsHourly(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	store := &listSpy{Store: blobs.NewSQLite(d.W, d.R, func() time.Time { return now.Add(-2 * time.Hour) }), failKey: "attachments/inst/a"}
	for _, k := range []string{"attachments/inst/a", "attachments/inst/b"} {
		if err := store.Put(bg, k, strings.NewReader("abc"), 3, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock, Blobs: store, Instance: "inst"}
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	left := func() string {
		var ks []string
		_ = store.Store.List(bg, "", func(k string, _ time.Time) error { ks = append(ks, k); return nil })
		return strings.Join(ks, " ")
	}
	if got := left(); got != "attachments/inst/a" {
		t.Fatalf("left %q", got)
	}
	store.failKey = ""
	now = now.Add(59 * time.Minute)
	if _, err := s.Tick(bg); err != nil || len(store.prefixes) != 2 || left() != "attachments/inst/a" {
		t.Fatalf("ran again within the hour: %v %v %q", err, store.prefixes, left())
	}
	now = now.Add(time.Minute)
	if _, err := s.Tick(bg); err != nil || len(store.prefixes) != 4 || left() != "" {
		t.Fatalf("hourly pass: %v %v %q", err, store.prefixes, left())
	}
}

// A failed listing doesn't count as the hourly pass: the next tick lists again.
func TestSweepFailedListRetriesNextTick(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := &listSpy{Store: blobs.NewSQLite(d.W, d.R, clock), failList: true}
	s := &sweep.Sweeper{Core: core.New(d.W, clock), DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock, Blobs: store, Instance: "inst"}
	if _, err := s.Tick(bg); err == nil {
		t.Fatal("a failed listing should be reported")
	}
	store.failList = false
	if _, err := s.Tick(bg); err != nil || len(store.prefixes) != 3 {
		t.Fatalf("not retried: %v %v", err, store.prefixes)
	}
}

// Review Focus 2: a thumbnail whose attachment exists is kept however old
// it is; one whose attachment is gone is deleted once it is older than
// OrphanAge; another instance's thumbnails are never touched.
func TestSweepKeepsLiveThumbsAndDeletesOrphans(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	owner, _ := c.EnsureOwner(bg)
	p, _ := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	tk, _ := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "x"})
	store := &listSpy{Store: blobs.NewSQLite(d.W, d.R, clock)}
	put := func(key string, age time.Duration) {
		t.Helper()
		was := now
		now = now.Add(-age)
		defer func() { now = was }()
		if err := store.Put(bg, key, strings.NewReader("abc"), 3, "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	live, err := c.AddAttachment(bg, owner.ID, core.AttachmentInput{TicketID: tk.ID, Name: "live.png", ContentType: "image/png", Size: 3, BlobKey: blobs.AttachmentKey("inst", idgen.New())})
	if err != nil {
		t.Fatal(err)
	}
	put(live.BlobKey, 2*time.Hour)
	gone, fresh := idgen.New(), idgen.New()
	put(blobs.ThumbKey("inst", live.ID), 3*time.Hour)
	put(blobs.ThumbKey("inst", gone), 2*time.Hour)
	put(blobs.ThumbKey("inst", fresh), 10*time.Minute)
	put(blobs.ThumbKey("other", gone), 2*time.Hour)
	for _, k := range []string{blobs.ThumbKey("inst", live.ID), blobs.ThumbKey("inst", gone)} {
		if err := blobs.ValidKey(k); err != nil || !strings.HasPrefix(k, "thumbs/inst/0") || k != strings.ToLower(k) {
			t.Fatalf("%q: %v", k, err)
		}
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock, Blobs: store, Instance: "inst"}
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	var left []string
	_ = store.Store.List(bg, "", func(k string, _ time.Time) error { left = append(left, k); return nil })
	wantKeys := []string{live.BlobKey, blobs.ThumbKey("inst", fresh), blobs.ThumbKey("inst", live.ID), blobs.ThumbKey("other", gone)}
	slices.Sort(wantKeys)
	slices.Sort(left)
	if got, want := strings.Join(left, " "), strings.Join(wantKeys, " "); got != want {
		t.Fatalf("left %v\nwant %v", got, want)
	}

	// The attachment goes: its thumbnail goes with the next pass.
	if _, err := c.DeleteAttachment(bg, owner.ID, live.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(sweep.OrphanAge)
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	left = nil
	_ = store.Store.List(bg, "thumbs/inst/", func(k string, _ time.Time) error { left = append(left, k); return nil })
	if len(left) != 0 {
		t.Fatalf("after delete: %v", left)
	}
}

// fetchWorld is a core on a clock the test moves, with a git runner that
// counts fetches and the most that ran at once.
type fetchEnv struct {
	c      *core.Core
	owner  gen.Actor
	now    *time.Time
	rec    *gittest.Recorder
	mu     sync.Mutex
	active int
	most   int
	s      *sweep.Sweeper
}

func newFetchEnv(t *testing.T) *fetchEnv {
	t.Helper()
	d := dbtest.New(t)
	now := time.Now().UTC()
	e := &fetchEnv{now: &now}
	e.c = core.New(d.W, func() time.Time { return now })
	e.c.Git = gitread.New()
	e.rec = gittest.Record(e.c.Git)
	e.c.Git.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args, env []string, limit int) ([]byte, error) {
			if gitread.Subcommand(args) != "fetch" {
				return next(ctx, dir, args, env, limit)
			}
			e.mu.Lock()
			e.active++
			e.most = max(e.most, e.active)
			e.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			defer func() { e.mu.Lock(); e.active--; e.mu.Unlock() }()
			return next(ctx, dir, args, env, limit)
		}
	})
	owner, err := e.c.EnsureOwner(bg)
	if err != nil {
		t.Fatal(err)
	}
	e.owner = owner
	e.s = &sweep.Sweeper{Core: e.c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}
	return e
}

// project makes a project whose repo is a clone of a bare remote, fetching
// every minutes.
func (e *fetchEnv) project(t *testing.T, key, repoDir string, minutes int64) {
	t.Helper()
	p, err := e.c.CreateProject(bg, e.owner.ID, core.ProjectInput{Key: key, Name: key})
	if err != nil {
		t.Fatal(err)
	}
	main := "main"
	if _, err := e.c.UpdateProject(bg, e.owner.ID, p.ID, core.ProjectPatch{RepoPath: &repoDir, MainBranch: &main, FetchMinutes: &minutes}); err != nil {
		t.Fatal(err)
	}
}

func clone(t *testing.T) string {
	t.Helper()
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	return gittest.Clone(t, bare).Dir
}

func TestSweepFetchesWhenDue(t *testing.T) {
	e := newFetchEnv(t)
	dir := clone(t)
	e.project(t, "VAQ", dir, 0)
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 0 {
		t.Fatalf("fetch_minutes 0 fetched %d times", n)
	}
	five := int64(5)
	p, _ := gen.New(e.s.DB).GetProjectByKey(bg, "VAQ")
	if _, err := e.c.UpdateProject(bg, e.owner.ID, p.ID, core.ProjectPatch{FetchMinutes: &five}); err != nil {
		t.Fatal(err)
	}
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 1 {
		t.Fatalf("never fetched: %d fetches", n)
	}
	e.s.Fetch(bg)
	*e.now = e.now.Add(4 * time.Minute)
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 1 {
		t.Fatalf("not yet due: %d fetches", n)
	}
	*e.now = e.now.Add(2 * time.Minute)
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 2 {
		t.Fatalf("due: %d fetches", n)
	}
}

// A fresh server counts the repo's own last fetch (FETCH_HEAD) as the last.
func TestSweepKeepsToTheLastFetchOnDisk(t *testing.T) {
	e := newFetchEnv(t)
	dir := clone(t)
	e.project(t, "VAQ", dir, 5)
	e.s.Fetch(bg)
	fresh := &sweep.Sweeper{Core: e.c, DB: e.s.DB, Logger: e.s.Logger, Now: e.s.Now}
	fresh.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 1 {
		t.Fatalf("a restart refetched: %d", n)
	}
	old := e.now.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, ".git", "FETCH_HEAD"), old, old); err != nil {
		t.Fatal(err)
	}
	fresh = &sweep.Sweeper{Core: e.c, DB: e.s.DB, Logger: e.s.Logger, Now: e.s.Now}
	fresh.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 2 {
		t.Fatalf("an old FETCH_HEAD wasn't due: %d", n)
	}
}

func TestSweepFetchesOneAtATimeAndOncePerRepo(t *testing.T) {
	e := newFetchEnv(t)
	one, two := clone(t), clone(t)
	e.project(t, "AAA", one, 5)
	e.project(t, "BBB", one, 5) // the same repo
	e.project(t, "CCC", two, 5)
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 2 {
		t.Fatalf("%d fetches for two repos", n)
	}
	if e.most != 1 {
		t.Fatalf("%d fetches at once", e.most)
	}
}

// A failure is remembered by the repo and tried again at the interval, not
// every tick.
func TestSweepFailedFetchWaitsForTheInterval(t *testing.T) {
	e := newFetchEnv(t)
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Git("remote", "add", "origin", t.TempDir()+"/nowhere.git")
	e.project(t, "VAQ", r.Dir, 5)
	e.s.Fetch(bg)
	e.s.Fetch(bg)
	if n := e.rec.Count("fetch"); n != 1 {
		t.Fatalf("%d fetches after a failure", n)
	}
	g, err := e.c.Git.Open(bg, r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := g.LastFetchError(); !ok || f.Line == "" {
		t.Fatalf("%+v %v", f, ok)
	}
}
