// Command vaquero is the Vaquero ticket tracker: its server and its CLI.
package main

import (
	"context"
	"database/sql"
	"errors"
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
  serve        run the server (the commands below start it when needed)
  version      print the release, commit and build date
  stop         stop the background server
  backup       list the database snapshots, or restore one
  export       write everything to a .tar.gz (vaquero export FILE)
  import       restore an export into an empty instance (vaquero import FILE)

tickets:
  projects     list projects
  project      create, show, edit or re-key a project
  tickets      list a project's tickets
  ticket       show, new, edit or move a ticket
  comment      add a comment to a ticket
  link         link two tickets (blocks, relates, duplicates)
  unlink       remove the links between two tickets
  attach       attach files to a ticket
  attachments  list a ticket's files
  detach       remove an attachment
  search       search tickets and comments
  activity     recent changes

agents:
  ready        list the tickets ready to be worked on
  claim        claim a ticket, with a lease (--lease 30m)
  heartbeat    extend your claim's lease
  release      give up your claim
  mcp          an MCP server on standard input and output, for agent clients

setup:
  token        create, list or revoke API tokens
  profile      add, list, use or remove servers to talk to

help:
  docs         list the help articles, or print one (vaquero docs cli)

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
	// The database is current: a marker from a failed upgrade is done with,
	// so the next upgrade from this version takes a fresh snapshot.
	if err := os.Remove(filepath.Join(backupDir, premigrateMarker)); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.WarnContext(ctx, "clear the snapshot marker", "err", err)
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
//
// A migration that fails leaves the database as it was, and the next boot
// would snapshot it again; a client retrying would then fill the kept
// snapshots with copies of one state and prune the older ones away. So the
// snapshot taken for a version is remembered (premigrateMarker) and reused
// while the database is still at that version; nothing can change it
// meanwhile, since no server runs on it. A boot that finds the database
// current, and a migration that succeeds, clear the marker (openDB).
func snapshotIfPending(ctx context.Context, w *sql.DB, backupDir string, now time.Time, logger *slog.Logger) error {
	applied, newest, err := appdb.Pending(w)
	if err != nil {
		return err
	}
	if applied == 0 || applied >= newest {
		return nil
	}
	marker := filepath.Join(backupDir, premigrateMarker)
	if name, ok := premigrateSnapshot(marker, applied); ok {
		if _, err := os.Stat(filepath.Join(backupDir, name)); err == nil {
			logger.InfoContext(ctx, "snapshot before migrating already taken", "from", applied, "to", newest, "path", filepath.Join(backupDir, name))
			return nil
		}
	}
	info, err := appdb.SnapshotNext(ctx, w, backupDir, now)
	if err != nil {
		hint := "check that " + backupDir + " can be written"
		if errors.Is(err, appdb.ErrLowSpace) {
			hint = "free some space in " + backupDir
		}
		return fmt.Errorf("the database needs migrating from version %d to %d, but the snapshot before it failed: %v; %s and start vaquero again", applied, newest, err, hint)
	}
	logger.InfoContext(ctx, "snapshot before migrating", "from", applied, "to", newest, "path", filepath.Join(backupDir, info.Name))
	if err := os.WriteFile(marker, fmt.Appendf(nil, "%d %s\n", applied, info.Name), 0o600); err != nil {
		logger.WarnContext(ctx, "remember the snapshot before migrating", "err", err)
	}
	if _, err := appdb.PruneBackups(backupDir, appdb.BackupKeep); err != nil {
		logger.WarnContext(ctx, "prune backups", "err", err)
	}
	return nil
}

// premigrateMarker, in the backups directory, names the snapshot taken
// before migrating from a version: "5 vaquero-20261120-120000.sqlite".
const premigrateMarker = ".before-migrating"

// premigrateSnapshot reads the marker: the snapshot's name when it was taken
// at version applied.
func premigrateSnapshot(path string, applied int64) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var v int64
	var name string
	if _, err := fmt.Sscanf(string(b), "%d %s", &v, &name); err != nil || v != applied || !appdb.BackupNamePattern.MatchString(name) {
		return "", false
	}
	return name, true
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
