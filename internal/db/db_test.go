package db_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func TestMigrateIsIdempotent(t *testing.T) {
	d := dbtest.New(t)
	if err := appdb.Migrate(d.W); err != nil {
		t.Fatal(err)
	}
	v, _ := appdb.Version(d.W)
	n, _ := appdb.Newest()
	if v != n {
		t.Fatalf("version %d, newest %d", v, n)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	for _, val := range []string{"a", "b"} {
		if err := d.Q.PutSetting(ctx, gen.PutSettingParams{Key: "k", Value: val, UpdatedAt: "2026-11-18T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := d.RQ.GetSetting(ctx, "k")
	if err != nil || s.Value != "b" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestReaderRefusesWrites(t *testing.T) {
	d := dbtest.New(t)
	if _, err := d.R.Exec(`DELETE FROM settings`); err == nil {
		t.Fatal("reader accepted a write")
	}
}

// Review Focus 3, plus the characters a file: URI treats specially.
func TestOpenOddPaths(t *testing.T) {
	for _, dir := range []string{"Application Support", "Work #2", "what?", "100%41"} {
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), dir, "vaquero", "vaquero.db")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			w, err := appdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			if err := appdb.Migrate(w); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("database not at %q: %v", path, err)
			}
			var mode string
			if err := w.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("journal_mode %q %v", mode, err)
			}
		})
	}
}

// Review Focus 5
func TestMigrateRefusesNewerDatabase(t *testing.T) {
	d := dbtest.New(t)
	if _, err := d.W.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(d.W); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("err = %v", err)
	}
}

func TestPendingReportsVersions(t *testing.T) {
	d, err := appdb.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := appdb.MigrateTo(d, 3); err != nil {
		t.Fatal(err)
	}
	applied, newest, err := appdb.Pending(d)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := appdb.Newest()
	if applied != 3 || newest != want {
		t.Fatalf("got (%d, %d), want (3, %d)", applied, newest, want)
	}
}
