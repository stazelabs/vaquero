// Command vaquero is the Vaquero ticket tracker: its server and its CLI.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/version"
)

const usage = `usage: vaquero <command>

server:
  serve      run the server (the commands below start it when needed)
  version    print the release, commit and build date

tickets:
  projects   list projects
  project    create, show, edit or re-key a project
  tickets    list a project's tickets
  ticket     show, new, edit or move a ticket
  comment    add a comment to a ticket
  link       link two tickets (blocks, relates, duplicates)
  unlink     remove the links between two tickets
  search     search tickets and comments
  activity   recent changes

agents:
  ready      list the tickets ready to be worked on
  claim      claim a ticket, with a lease (--lease 30m)
  heartbeat  extend your claim's lease
  release    give up your claim
  mcp        an MCP server on standard input and output, for agent clients

setup:
  token      create, list or revoke API tokens
  profile    add, list, use or remove servers to talk to

help:
  docs       list the help articles, or print one (vaquero docs cli)

Commands that talk to a server take --profile NAME, --agent NAME and --json.
Every command is in: vaquero docs cli
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is main, testable: it returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version.Line())
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	case "serve":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(stderr, "config:", err)
			return 1
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, cfg, newLogger(cfg, stderr)); err != nil {
			fmt.Fprintln(stderr, "serve:", err)
			return 1
		}
	default:
		if fn, ok := cliCommands[args[0]]; ok {
			return fn(context.Background(), stdout, stderr, args[1:])
		}
		fmt.Fprint(stderr, usage)
		return 2
	}
	return 0
}

// openDB opens the writer (migrated) and a reader pool on path, creating
// the directory as needed.
func openDB(ctx context.Context, path, backupDir string, now time.Time, logger *slog.Logger) (w, r *sql.DB, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	w, err = appdb.Open(path)
	if err != nil {
		return nil, nil, err
	}
	if err := snapshotIfPending(ctx, w, backupDir, now, logger); err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	if err := appdb.Migrate(w); err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	r, err = appdb.OpenReader(path, 8)
	if err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	return w, r, nil
}

// snapshotIfPending copies the database into backupDir before migrations
// run on it, so an upgrade can always be undone. A fresh database and an
// up-to-date one need none. When the copy fails, nothing is migrated.
func snapshotIfPending(ctx context.Context, w *sql.DB, backupDir string, now time.Time, logger *slog.Logger) error {
	applied, newest, err := appdb.Pending(w)
	if err != nil {
		return err
	}
	if applied == 0 || applied >= newest {
		return nil
	}
	info, err := appdb.Snapshot(ctx, w, backupDir, now)
	if err != nil {
		return fmt.Errorf("the database needs migrating from version %d to %d, but the snapshot before it failed: %v; free some space in %s and start vaquero again", applied, newest, err, backupDir)
	}
	logger.InfoContext(ctx, "snapshot before migrating", "from", applied, "to", newest, "path", filepath.Join(backupDir, info.Name))
	if _, err := appdb.PruneBackups(backupDir, appdb.BackupKeep); err != nil {
		logger.WarnContext(ctx, "prune backups", "err", err)
	}
	return nil
}

// newLogger builds the process logger: JSON when cfg.LogFormat is "json",
// else text; the level comes from cfg.LogLevel (Info when unreadable).
func newLogger(cfg config.Config, out io.Writer) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(cfg.LogLevel))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	return slog.New(slog.NewTextHandler(out, opts))
}
