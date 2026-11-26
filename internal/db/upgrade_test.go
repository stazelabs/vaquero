package db_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// backfilled are the values a migration fills in on purpose, by table and
// column: the dump reads each through its expression, which leaves out what
// the backfill adds, and backfillChecks then checks what it added. 0008 gives
// v0.1.x link events their target's id (payload.to_id).
var backfilled = map[string]map[string]string{
	"events": {"payload": `CASE WHEN kind IN ('link.created', 'link.deleted') THEN json_remove(payload, '$.to_id') ELSE payload END`},
}

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
			before := dumpTables(t, d, nil)
			targets := linkTargets(t, d)
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
			// Every row keeps every value it had, in the columns it had.
			for _, line := range tableDiff(before, dumpTables(t, d, columnsOf(before))) {
				t.Error(line)
			}
			checkLinkTargets(t, d, targets)
			hits, err := appdb.SearchTickets(t.Context(), d, `"lasso"*`, 10)
			if err != nil || len(hits) != 1 {
				t.Fatalf("search for lasso: %d hits, %v", len(hits), err)
			}
		})
	}
}

// dump is every row of every tracker table, as quoted SQL values, in key
// order, with the columns it was read with.
type dump struct {
	cols map[string][]string
	rows map[string][]string
}

// dumpTables reads each table's rows: through cols when given (the columns a
// fixture had, so a migration's new columns don't count as changes), else
// through every column the table has now. The search indexes are left out;
// the search check covers them.
func dumpTables(t *testing.T, d *sql.DB, cols map[string][]string) dump {
	t.Helper()
	out := dump{cols: map[string][]string{}, rows: map[string][]string{}}
	if cols == nil {
		cols = map[string][]string{}
		names, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%fts%' AND name != 'goose_db_version'`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for names.Next() {
			var n string
			names.Scan(&n)
			tables = append(tables, n)
		}
		names.Close()
		for _, table := range tables {
			info, err := d.Query(`SELECT name FROM pragma_table_info(?)`, table)
			if err != nil {
				t.Fatal(err)
			}
			for info.Next() {
				var c string
				info.Scan(&c)
				cols[table] = append(cols[table], c)
			}
			info.Close()
		}
	}
	for table, cs := range cols {
		quoted := make([]string, len(cs))
		for i, c := range cs {
			expr := c
			if e, ok := backfilled[table][c]; ok {
				expr = e
			}
			quoted[i] = "quote(" + expr + ")"
		}
		rows, err := d.Query("SELECT " + strings.Join(quoted, " || '|' || ") + " FROM " + table + " ORDER BY " + orderOf(t, d, table))
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		for rows.Next() {
			var r string
			rows.Scan(&r)
			out.rows[table] = append(out.rows[table], r)
		}
		rows.Close()
		out.cols[table] = cs
	}
	return out
}

// orderOf is a table's declared primary key, in key order, or rowid when it
// declares none. A WITHOUT ROWID table (blobs) has no rowid to order by.
func orderOf(t *testing.T, d *sql.DB, table string) string {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var pk []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		pk = append(pk, c)
	}
	if len(pk) == 0 {
		return "rowid"
	}
	return strings.Join(pk, ", ")
}

// linkTargets is each link event's payload to_id, by seq ("" for none).
func linkTargets(t *testing.T, d *sql.DB) map[int64]string {
	t.Helper()
	rows, err := d.Query(`SELECT seq, coalesce(json_extract(payload, '$.to_id'), '') FROM events WHERE kind IN ('link.created', 'link.deleted')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var seq int64
		var to string
		if err := rows.Scan(&seq, &to); err != nil {
			t.Fatal(err)
		}
		out[seq] = to
	}
	return out
}

// checkLinkTargets is the rule for the one payload value a migration
// changes: a to_id a link event had stays as it was, and one 0008 added is
// the target of the event's link row (every fixture link still exists).
func checkLinkTargets(t *testing.T, d *sql.DB, before map[int64]string) {
	t.Helper()
	after := linkTargets(t, d)
	if len(after) != len(before) || len(after) == 0 {
		t.Fatalf("%d link events after migrating, %d before", len(after), len(before))
	}
	for seq, was := range before {
		got := after[seq]
		if was != "" {
			if got != was {
				t.Errorf("event %d: to_id %q became %q", seq, was, got)
			}
			continue
		}
		var want string
		err := d.QueryRow(`SELECT l.to_id FROM events e JOIN ticket_links l ON l.id = json_extract(e.payload, '$.link_id') WHERE e.seq = ?`, seq).Scan(&want)
		if err != nil {
			t.Fatalf("event %d: its link: %v", seq, err)
		}
		if got != want {
			t.Errorf("event %d: backfilled to_id %q, want the link's target %q", seq, got, want)
		}
	}
}

func columnsOf(d dump) map[string][]string { return d.cols }

// tableDiff lists the rows in before that after lost or changed, table by
// table. Rows a later migration adds are the counts check's business.
func tableDiff(before, after dump) []string {
	var out []string
	for table, rows := range before.rows {
		got := after.rows[table]
		for i, r := range rows {
			if i >= len(got) {
				out = append(out, fmt.Sprintf("%s: row %d is gone: %s", table, i+1, r))
				break
			}
			if got[i] != r {
				out = append(out, fmt.Sprintf("%s: row %d changed\n  before: %s\n  after:  %s", table, i+1, r, got[i]))
			}
		}
	}
	return out
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

func TestTableDumpCatchesChangedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vaquero.db")
	copyFixture(t, filepath.Join(fixtureDir, "v0.1.0.sqlite"), path)
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	before := dumpTables(t, d, nil)
	if _, err := d.Exec("UPDATE tickets SET title = 'changed' WHERE number = 1"); err != nil {
		t.Fatal(err)
	}
	if diff := tableDiff(before, dumpTables(t, d, columnsOf(before))); len(diff) == 0 {
		t.Fatal("a changed title went unnoticed")
	}
}
