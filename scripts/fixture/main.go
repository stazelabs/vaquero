// Command fixture writes testdata/upgrade/v{VERSION}.sqlite: a database with
// a little of everything, made through internal/core at the current schema,
// and v{VERSION}.json with its schema version and row counts. Run it (make
// fixture VERSION=X) before each release and commit both files; the test
// TestFixturesMigrateToHead then proves every released database migrates
// to head with nothing lost. Never edit or delete a fixture.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/core/coreseed"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// tables are the ones whose row counts the fixture records.
var tables = []string{
	"settings", "actors", "api_tokens", "projects", "statuses", "milestones",
	"tickets", "tags", "ticket_tags", "comments", "ticket_links", "attachments", "blobs", "events", "mcp_calls",
	"plans", "plan_revisions", "decisions", "decision_tickets", "approvals",
}

func main() {
	version := flag.String("version", "", "the release this fixture stands for, like 0.1.0")
	dir := flag.String("dir", "testdata/upgrade", "where to write it")
	flag.Parse()
	if *version == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/fixture -version X")
		os.Exit(2)
	}
	if err := write(context.Background(), *dir, *version); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

func write(ctx context.Context, dir, version string) error {
	base := filepath.Join(dir, "v"+version)
	if _, err := os.Stat(base + ".sqlite"); err == nil {
		return fmt.Errorf("%s.sqlite exists; fixtures are never replaced", base)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "vaquero-fixture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "vaquero.db")
	w, err := appdb.Open(path)
	if err != nil {
		return err
	}
	defer w.Close()
	if err := appdb.Migrate(w); err != nil {
		return err
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(w, clock)
	c.BlobStore = "sqlite"
	// A served database carries its instance id, which names its blob keys;
	// the fixture records one the same way serve does.
	const instance = "fixture"
	if err := gen.New(w).PutSetting(ctx, gen.PutSettingParams{Key: "instance_id", Value: instance, UpdatedAt: timefmt.Format(now)}); err != nil {
		return err
	}
	store := blobs.NewSQLite(w, w, clock)
	if err := coreseed.Seed(ctx, c, gen.New(w), store, instance, now); err != nil {
		return err
	}
	schema, err := appdb.Version(w)
	if err != nil {
		return err
	}
	counts := map[string]int64{}
	for _, t := range tables {
		var n int64
		if err := w.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return fmt.Errorf("count %s: %w", t, err)
		}
		counts[t] = n
	}
	// VACUUM INTO writes one self-contained file with the WAL folded in.
	if _, err := w.ExecContext(ctx, "VACUUM INTO ?", base+".sqlite"); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]any{"version": version, "schema": schema, "counts": counts}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s.sqlite and %s.json (schema %d)\n", base, base, schema)
	return nil
}
