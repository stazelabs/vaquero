// Package handlers holds the HTTP handlers. Each is func(Deps) http.HandlerFunc.
//
// Handlers read through Deps.R and change the tracker only through
// Deps.Core (TestHandlersNeverWriteDirectly holds it).
package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/web/middleware"
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
	// Touch records an API token's last use: the API middleware's one write.
	Touch middleware.Toucher
	// MCPCalls keeps the MCP activity log (mcp_calls, not a tracker table).
	MCPCalls MCPCallLog
	// RenewLocalToken (nil in tests) makes sure the local token file holds a
	// live token; called after a revoke, in case it was the local one.
	RenewLocalToken func(context.Context) error
	// BackupDir is where a snapshot is saved before a project is deleted.
	BackupDir string
	Now       func() time.Time
	// Blobs holds attachment bytes, written only through it; Instance is
	// this database's instance id, which names the blob keys.
	Blobs    blobs.Store
	Instance string
	// Git reads projects' repos; the same value as Core.Git.
	Git *gitread.Repos
	// Files compares approvals' pinned files with main for readiness
	// (readiness.NewFiles over Git, kept for its cache); nil makes one
	// over Git each time it is asked for.
	Files readiness.Files

	// Shutdown closes when the server starts shutting down, ending open
	// live-update streams. StreamPoll is how often a stream checks for
	// changes (one second when zero).
	Shutdown   <-chan struct{}
	StreamPoll time.Duration
}

// MCPCallLog records an MCP tool call (mcpserver.CallLog).
type MCPCallLog interface {
	RecordMCPCall(ctx context.Context, arg gen.RecordMCPCallParams) error
}

// files is the readiness file checker: none without git (pins are then
// unchecked, never passed).
func (d Deps) files() readiness.Files {
	if d.Git == nil {
		return nil
	}
	if d.Files != nil {
		return d.Files
	}
	return readiness.NewFiles(d.Git)
}
