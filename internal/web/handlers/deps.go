// Package handlers holds the HTTP handlers. Each is func(Deps) http.HandlerFunc.
package handlers

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// Deps is everything a handler may use.
type Deps struct {
	Cfg    config.Config
	DB     *sql.DB      // writer pool, for transactions and /healthz
	Q      *gen.Queries // writer
	R      *gen.Queries // reader: use it for reads on request paths
	Logger *slog.Logger
	Now    func() time.Time
}
