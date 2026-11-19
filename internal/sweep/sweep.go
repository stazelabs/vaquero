// Package sweep is the background ticker of a running server: every
// Every it ends lapsed claims (core.ExpireClaims) and deletes MCP activity
// older than KeepMCPCalls. serve starts it and stops it before draining.
package sweep

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

const (
	// Every is how often the sweep runs.
	Every = 30 * time.Second
	// KeepMCPCalls is how long the MCP activity log keeps a call.
	KeepMCPCalls = 30 * 24 * time.Hour
)

// Sweeper runs the sweep. DB is the writer pool, for the log's pruning.
type Sweeper struct {
	Core   *core.Core
	DB     *sql.DB
	Logger *slog.Logger
	Now    func() time.Time
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
	return n, nil
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
