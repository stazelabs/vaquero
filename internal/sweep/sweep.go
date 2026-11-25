// Package sweep is the background ticker of a running server: every
// Every it ends lapsed claims (core.ExpireClaims), deletes MCP activity
// older than KeepMCPCalls, and deletes orphaned attachment bytes: objects
// under this instance's attachments/{instance}/ prefix with no attachments
// row, older than OrphanAge (an upload stores its bytes before core writes
// the row, so a younger object may be one in flight). serve starts it and
// stops it before draining.
package sweep

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

	startOnce, stopOnce sync.Once
	cancel              context.CancelFunc
	done                chan struct{}
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

// deleteOrphans removes this instance's attachment objects that no row
// references and that are older than OrphanAge. It lists first and deletes
// after, so no backend is changed while it is being listed.
func (s *Sweeper) deleteOrphans(ctx context.Context) error {
	if s.Blobs == nil || s.Instance == "" {
		return nil
	}
	cutoff := s.Now().Add(-OrphanAge)
	var old []string
	err := s.Blobs.List(ctx, "attachments/"+s.Instance+"/", func(key string, modified time.Time) error {
		if modified.Before(cutoff) {
			old = append(old, key)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("list attachment objects: %w", err)
	}
	q := gen.New(s.DB)
	deleted := 0
	for _, key := range old {
		_, err := q.AttachmentByBlobKey(ctx, key)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("attachment by blob key: %w", err)
		}
		if err := s.Blobs.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete orphan: %w", err)
		}
		deleted++
	}
	if deleted > 0 {
		s.Logger.InfoContext(ctx, "orphaned attachments deleted", "count", deleted)
	}
	return nil
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
		}
	})
}
