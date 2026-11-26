package archive_test

import (
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
)

// TestExportImportKeepsMeta: an event's provenance (events.meta) survives
// an export and an import unchanged, and an event with none keeps {}.
func TestExportImportKeepsMeta(t *testing.T) {
	d, store := seeded(t)
	c := core.New(d.W, func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) })
	owner, err := c.EnsureOwner(bg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := core.WithOrigin(bg, core.Origin{Surface: "mcp", Client: "vaquero-mcp/0.1.6", Harness: "codex/1.2", Model: "gpt-x", Run: "run-l"})
	if _, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "META", Name: "Meta"}); err != nil {
		t.Fatal(err)
	}
	const want = `{"client":"vaquero-mcp/0.1.6","harness":"codex/1.2","model":"gpt-x","run":"run-l","surface":"mcp"}`
	var stored string
	if err := d.R.QueryRow(`SELECT meta FROM events ORDER BY seq DESC LIMIT 1`).Scan(&stored); err != nil || stored != want {
		t.Fatalf("stored %q %v, want %q", stored, err, want)
	}

	loaded, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	// The seed records one change with provenance too; the import keeps
	// every event's meta as exported.
	var withMeta, empty, mine int
	if err := loaded.R.QueryRow(`SELECT count(*) FILTER (WHERE meta <> '{}'), count(*) FILTER (WHERE meta = '{}'),
		count(*) FILTER (WHERE meta = ?) FROM events`, want).Scan(&withMeta, &empty, &mine); err != nil {
		t.Fatal(err)
	}
	if withMeta != 2 || mine != 1 || empty == 0 {
		t.Fatalf("%d events with meta (%d this one), %d without", withMeta, mine, empty)
	}
}
