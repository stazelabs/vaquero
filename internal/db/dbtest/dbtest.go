// Package dbtest gives each test an isolated, migrated database by copying a
// template that is migrated once per test process.
package dbtest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// DB is a test database: writer and reader pools plus queries over each.
type DB struct {
	W, R  *sql.DB
	Q, RQ *gen.Queries
}

var (
	once   sync.Once
	tplDB  []byte
	tplErr error
	// tplDir is where the template was built; it is gone once built.
	tplDir string
)

// template returns the migrated database's bytes. The file it was built in
// is read into memory and its directory removed, so a test run leaves
// nothing in the temp directory.
func template() ([]byte, error) {
	once.Do(func() {
		dir, err := os.MkdirTemp("", "vaquero-db-template-")
		if err != nil {
			tplErr = err
			return
		}
		tplDir = dir
		defer os.RemoveAll(dir)
		p := filepath.Join(dir, "template.db")
		w, err := appdb.Open(p)
		if err != nil {
			tplErr = err
			return
		}
		if err := appdb.Migrate(w); err != nil {
			w.Close()
			tplErr = fmt.Errorf("migrate template: %w", err)
			return
		}
		if err := appdb.Checkpoint(w); err != nil {
			w.Close()
			tplErr = err
			return
		}
		if err := w.Close(); err != nil {
			tplErr = err
			return
		}
		tplDB, tplErr = os.ReadFile(p)
	})
	return tplDB, tplErr
}

// New returns a fresh migrated database unique to t.
func New(t testing.TB) DB {
	t.Helper()
	tpl, err := template()
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(dst, tpl, 0o600); err != nil {
		t.Fatalf("dbtest: copy: %v", err)
	}
	w, err := appdb.Open(dst)
	if err != nil {
		t.Fatalf("dbtest: open writer: %v", err)
	}
	r, err := appdb.OpenReader(dst, 4)
	if err != nil {
		w.Close()
		t.Fatalf("dbtest: open reader: %v", err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return DB{W: w, R: r, Q: gen.New(w), RQ: gen.New(r)}
}
