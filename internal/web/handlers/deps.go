// Package handlers holds the HTTP handlers. Each is func(Deps) http.HandlerFunc.
//
// Handlers read through Deps.R and change the tracker only through
// Deps.Core (TestHandlersNeverWriteDirectly holds it).
package handlers

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// Deps is everything a handler may use.
type Deps struct {
	Cfg    config.Config
	DB     *sql.DB      // writer pool; only /healthz pings it
	R      *gen.Queries // reader: every read on a request path
	RDB    *sql.DB      // the reader pool itself, for the hand-written search query
	Core   *core.Core   // every change
	Actor  gen.Actor    // who changes things: the local owner in local mode
	Logger *slog.Logger
	Now    func() time.Time

	// Shutdown closes when the server starts shutting down, ending open
	// live-update streams. StreamPoll is how often a stream checks for
	// changes (one second when zero).
	Shutdown   <-chan struct{}
	StreamPoll time.Duration
}
