package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func init() {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(log.New(io.Discard, "", 0))
	if err := goose.SetDialect("sqlite3"); err != nil {
		panic(err)
	}
}

// Migrate applies every pending embedded migration. Called once at boot.
//
// It first refuses a database whose applied version is beyond the newest
// embedded migration, one made by a newer build. goose alone would find
// nothing pending and boot on a schema this build doesn't know, and a later
// migration would read as already applied.
func Migrate(db *sql.DB) error {
	newest, err := Newest()
	if err != nil {
		return err
	}
	applied, err := goose.GetDBVersion(db)
	if err != nil {
		return fmt.Errorf("goose version: %w", err)
	}
	if applied > newest {
		return fmt.Errorf("the database was written by a newer vaquero (migration %d; this build knows up to %d); install that version or newer", applied, newest)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// Version returns the applied goose version. SchemaStatus says more.
func Version(db *sql.DB) (int64, error) {
	return goose.GetDBVersion(db)
}

// MigrateTo applies migrations up to version. A test of a data migration
// (0002 on) uses it to stage rows before that migration runs.
func MigrateTo(db *sql.DB, version int64) error {
	return goose.UpTo(db, "migrations", version)
}

// migrationFiles maps each embedded migration's version to its file name,
// read from the names (0001_init.sql is version 1). Every reader of the
// embedded set goes through it.
func migrationFiles() (map[int64]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	files := make(map[int64]string, len(entries))
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		files[v] = e.Name()
	}
	return files, nil
}

// Newest is the highest version among the embedded migrations: the schema
// this build expects.
func Newest() (int64, error) {
	files, err := migrationFiles()
	if err != nil {
		return 0, err
	}
	var newest int64
	for v := range files {
		newest = max(newest, v)
	}
	return newest, nil
}
