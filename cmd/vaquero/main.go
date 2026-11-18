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

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/version"
)

const usage = `usage: vaquero <command>

commands:
  serve      run the server
  version    print the release, commit and build date
  help       show this message
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
		fmt.Fprint(stderr, usage)
		return 2
	}
	return 0
}

// openDB opens the writer (migrated) and a reader pool on path, creating
// the directory as needed.
func openDB(path string) (w, r *sql.DB, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	w, err = appdb.Open(path)
	if err != nil {
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
