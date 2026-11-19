package db_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
)

// fixtureDir holds a database from every release (make fixture). Each must
// migrate to head with nothing lost: the promise that upgrading never needs
// manual steps.
const fixtureDir = "../../testdata/upgrade"

// grownTables may hold more rows after migrating than the fixture recorded,
// because a later migration seeds them. Empty until one does.
var grownTables = map[string]bool{}

type fixtureInfo struct {
	Version string           `json:"version"`
	Schema  int64            `json:"schema"`
	Counts  map[string]int64 `json:"counts"`
}

func TestFixturesMigrateToHead(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "*.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no upgrade fixtures; run make fixture VERSION=…")
	}
	newest, err := appdb.Newest()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(strings.TrimSuffix(f, ".sqlite") + ".json")
			if err != nil {
				t.Fatalf("fixture without its counts: %v", err)
			}
			var info fixtureInfo
			if err := json.Unmarshal(raw, &info); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "vaquero.db")
			copyFixture(t, f, path)
			d, err := appdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if v, _ := appdb.Version(d); v != info.Schema {
				t.Fatalf("fixture is at schema %d, its JSON says %d", v, info.Schema)
			}
			if err := appdb.Migrate(d); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			if v, _ := appdb.Version(d); v != newest {
				t.Fatalf("migrated to %d, want %d", v, newest)
			}
			var ok string
			if err := d.QueryRow("PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
				t.Fatalf("integrity_check: %q %v", ok, err)
			}
			rows, err := d.Query("PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				t.Error("foreign_key_check found broken references")
			}
			rows.Close()
			for table, want := range info.Counts {
				var got int64
				if err := d.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil {
					t.Errorf("%s: %v", table, err)
					continue
				}
				if got != want && !(grownTables[table] && got > want) {
					t.Errorf("%s has %d rows after migrating, want %d", table, got, want)
				}
			}
			hits, err := appdb.SearchTickets(t.Context(), d, `"lasso"*`, 10)
			if err != nil || len(hits) != 1 {
				t.Fatalf("search for lasso: %d hits, %v", len(hits), err)
			}
		})
	}
}

func copyFixture(t *testing.T, from, to string) {
	t.Helper()
	src, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
}
