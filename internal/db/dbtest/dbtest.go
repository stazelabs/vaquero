// Package dbtest gives each test an isolated, migrated database by copying a
// template that is migrated once per test process.
package dbtest

import (
	"database/sql"
	"fmt"
	"io"
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
	once    sync.Once
	tplPath string
	tplErr  error
)

func template() (string, error) {
	once.Do(func() {
		dir, err := os.MkdirTemp("", "vaquero-db-template-")
		if err != nil {
			tplErr = err
			return
		}
		p := filepath.Join(dir, "template.db")
		w, err := appdb.Open(p)
		if err != nil {
			tplErr = err
			return
		}
		defer w.Close()
		if err := appdb.Migrate(w); err != nil {
			tplErr = fmt.Errorf("migrate template: %w", err)
			return
		}
		if err := appdb.Checkpoint(w); err != nil {
			tplErr = err
			return
		}
		tplPath = p
	})
	return tplPath, tplErr
}

// New returns a fresh migrated database unique to t.
func New(t testing.TB) DB {
	t.Helper()
	src, err := template()
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "test.db")
	if err := copyFile(src, dst); err != nil {
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
