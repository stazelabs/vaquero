// Package sweep is the background ticker of a running server: every
// Every it ends lapsed claims (core.ExpireClaims), deletes MCP activity
// older than KeepMCPCalls, and deletes orphaned attachment objects older
// than OrphanAge (an upload stores its bytes before core writes the row, so
// a younger object may be one in flight): bytes under this instance's
// attachments/{instance}/ prefix that no attachments row names, and
// previews under thumbs/{instance}/{id} whose attachment id has no row. A
// preview whose attachment exists is kept however old it is. serve starts
// it and stops it before draining.
//
// The sweep also fetches (Fetch, VAQ-136): a project with a repository and
// fetch_minutes above 0 has its remote fetched when its last fetch is that
// old. It is off by default and the only thing here that uses the network.
// A fetch runs apart from the tick, one at a time, so a slow remote never
// delays the claims. Stop cancels a running fetch and returns after git
// has exited.
package sweep

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

const (
	// Every is how often the sweep runs.
	Every = 30 * time.Second
	// KeepMCPCalls is how long the MCP activity log keeps a call.
	KeepMCPCalls = 30 * 24 * time.Hour
	// OrphanAge is how old an unreferenced attachment object must be
	// before the sweep deletes it.
	OrphanAge = time.Hour
)

// Sweeper runs the sweep. DB is the writer pool, for the log's pruning.
type Sweeper struct {
	Core   *core.Core
	DB     *sql.DB
	Logger *slog.Logger
	Now    func() time.Time
	// Blobs and Instance name the attachment store and this instance's
	// prefix in it; a nil Blobs skips the orphan pass.
	Blobs    blobs.Store
	Instance string
	// Interval overrides Every; OnTick runs after each tick (both for tests).
	Interval time.Duration
	OnTick   func()

	lastOrphans         time.Time // when the orphan pass last ran
	startOnce, stopOnce sync.Once
	cancel              context.CancelFunc
	done                chan struct{}
	fetches             sync.WaitGroup
	fetchRunning        atomic.Bool

	mu    sync.Mutex
	tried map[string]time.Time // repo and remote → when this sweeper last fetched it
}

// Tick runs one sweep and returns how many claims it ended.
func (s *Sweeper) Tick(ctx context.Context) (int, error) {
	n, err := s.Core.ExpireClaims(ctx)
	if err != nil {
		return n, err
	}
	if _, err := gen.New(s.DB).PruneMCPCalls(ctx, timefmt.Format(s.Now().Add(-KeepMCPCalls))); err != nil {
		return n, err
	}
	if err := s.deleteOrphans(ctx); err != nil {
		return n, err
	}
	return n, nil
}

// Fetch fetches every repository that is due and returns how many fetches
// it ran. A project is due when its fetch_minutes is above 0 and its repo's
// last fetch (the later of this sweeper's own attempt, successful or not,
// and the repo's FETCH_HEAD) is at least that old. Archived projects are
// skipped; projects that share a repository share one fetch. They run one
// at a time. A failure is kept by the repo (gitread.Repo.LastFetchError)
// and logged at warn; the next try is a full interval later.
func (s *Sweeper) Fetch(ctx context.Context) int {
	if s.Core == nil || s.Core.Git == nil || !s.Core.Git.Available() {
		return 0
	}
	projects, err := gen.New(s.DB).ListProjects(ctx)
	if err != nil {
		s.Logger.ErrorContext(ctx, "list projects for fetch", "err", err)
		return 0
	}
	ran := 0
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		if p.FetchMinutes <= 0 || p.RepoPath == "" || p.ArchivedAt.Valid {
			continue
		}
		repo, err := s.Core.Git.Open(ctx, p.RepoPath, p.MainBranch, p.Remote)
		if err != nil || repo.Remote == "" {
			continue
		}
		key := repo.Path + "\x00" + repo.Remote
		now := s.Now()
		last, _ := repo.LastFetch()
		s.mu.Lock()
		if t := s.tried[key]; t.After(last) {
			last = t
		}
		s.mu.Unlock()
		if !last.IsZero() && now.Sub(last) < time.Duration(p.FetchMinutes)*time.Minute {
			continue
		}
		s.mu.Lock()
		if s.tried == nil {
			s.tried = map[string]time.Time{}
		}
		s.tried[key] = now
		s.mu.Unlock()
		ran++
		if err := repo.FetchOwned(ctx); err != nil && ctx.Err() == nil {
			s.Logger.WarnContext(ctx, "fetch", "project", p.Key, "err", err)
		}
	}
	return ran
}

// startFetch runs Fetch on its own goroutine unless one is running.
func (s *Sweeper) startFetch(ctx context.Context) {
	if !s.fetchRunning.CompareAndSwap(false, true) {
		return
	}
	s.fetches.Go(func() {
		defer s.fetchRunning.Store(false)
		s.Fetch(ctx)
	})
}

// deleteOrphans removes this instance's attachment bytes and previews
// that no row references and that are older than OrphanAge, at most once
// per OrphanAge (the first tick runs it). It lists both prefixes first and
// deletes after, so no backend is changed while it is being listed. A key
// that can't be checked or deleted is logged and skipped; the next pass
// tries it again.
func (s *Sweeper) deleteOrphans(ctx context.Context) error {
	if s.Blobs == nil || s.Instance == "" {
		return nil
	}
	now := s.Now()
	if !s.lastOrphans.IsZero() && now.Sub(s.lastOrphans) < OrphanAge {
		return nil
	}
	cutoff := now.Add(-OrphanAge)
	q := gen.New(s.DB)
	// Each pass lists one prefix; referenced reports whether a row still
	// needs the object at key.
	type pass struct {
		prefix     string
		referenced func(key string) (bool, error)
	}
	passes := []pass{
		{"attachments/" + s.Instance + "/", func(key string) (bool, error) {
			_, err := q.AttachmentByBlobKey(ctx, key)
			return found(err)
		}},
		{"thumbs/" + s.Instance + "/", func(key string) (bool, error) {
			// The last segment is the attachment id, lower case; ids are
			// stored in upper case.
			id := strings.ToUpper(key[strings.LastIndexByte(key, '/')+1:])
			_, err := q.GetAttachment(ctx, id)
			return found(err)
		}},
	}
	type orphan struct {
		key        string
		referenced func(string) (bool, error)
	}
	var old []orphan
	for _, p := range passes {
		err := s.Blobs.List(ctx, p.prefix, func(key string, modified time.Time) error {
			if modified.Before(cutoff) {
				old = append(old, orphan{key, p.referenced})
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("list attachment objects: %w", err)
		}
	}
	s.lastOrphans = now // only listings that worked count as the pass
	deleted, failed := 0, 0
	for _, o := range old {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ref, err := o.referenced(o.key)
		if err != nil {
			s.Logger.ErrorContext(ctx, "check orphan", "key", o.key, "err", err)
			failed++
			continue
		}
		if ref {
			continue
		}
		if err := s.Blobs.Delete(ctx, o.key); err != nil {
			s.Logger.ErrorContext(ctx, "delete orphan", "key", o.key, "err", err)
			failed++
			continue
		}
		deleted++
	}
	if deleted > 0 || failed > 0 {
		s.Logger.InfoContext(ctx, "orphaned attachments swept", "deleted", deleted, "failed", failed)
	}
	return nil
}

// found turns a row lookup's error into whether the row exists.
func found(err error) (bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Start runs Tick every Interval (Every) until Stop.
func (s *Sweeper) Start() {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel, s.done = cancel, make(chan struct{})
		every := s.Interval
		if every <= 0 {
			every = Every
		}
		go func() {
			defer close(s.done)
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				if n, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
					s.Logger.ErrorContext(ctx, "sweep", "err", err)
				} else if n > 0 {
					s.Logger.InfoContext(ctx, "claims lapsed", "count", n)
				}
				s.startFetch(ctx)
				if s.OnTick != nil {
					s.OnTick()
				}
			}
		}()
	})
}

// Stop ends the ticker and waits for a running tick to finish. It is safe
// to call more than once, and before Start.
func (s *Sweeper) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
			s.fetches.Wait()
		}
	})
}
