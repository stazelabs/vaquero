package archive

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// skipSettings are the settings rows that describe this instance, not its
// data: import writes its own.
var skipSettings = []string{"instance_id", "blob_store"}

// nulled are columns written as null whatever they hold: live claims don't
// travel.
var nulled = map[string][]string{
	"tickets": {"claim_expires_at", "claim_prev_status_id", "claimed_by"},
}

// entryKey is the blob_key an attachment is exported with: the name of its
// tar entry. The stored key names this instance, which doesn't travel;
// import stores the bytes under its own instance's key.
func entryKey(id any) (any, error) {
	s, ok := id.(string)
	if !ok {
		return nil, fmt.Errorf("attachment id %v isn't text", id)
	}
	return "attachments/" + s, nil
}

// column is one row of pragma_table_info.
type column struct {
	name, typ string
	pk        int
}

// dump writes export.json to buf, reading through tx.
func dump(ctx context.Context, tx *sql.Tx, buf *bytes.Buffer, h Header) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	buf.WriteString(`{"format":`)
	if err := writeString(buf, enc, h.Format); err != nil {
		return err
	}
	fmt.Fprintf(buf, `,"format_version":%d,"schema":%d,"exported_at":`, h.FormatVersion, h.Schema)
	if err := writeString(buf, enc, h.ExportedAt); err != nil {
		return err
	}
	buf.WriteString(`,"vaquero_version":`)
	if err := writeString(buf, enc, h.VaqueroVersion); err != nil {
		return err
	}
	for _, t := range Tables {
		buf.WriteString(`,"` + t + `":`)
		if err := dumpTable(ctx, tx, buf, enc, t); err != nil {
			return fmt.Errorf("export %s: %w", t, err)
		}
	}
	buf.WriteString("}\n")
	return nil
}

// columns reads a table's columns, refusing a BLOB one.
func columns(ctx context.Context, tx *sql.Tx, table string) ([]column, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name, type, pk FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ, &c.pk); err != nil {
			return nil, err
		}
		if strings.EqualFold(c.typ, "BLOB") {
			return nil, fmt.Errorf("column %s is a BLOB, which an export can't hold", c.name)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no such table")
	}
	return cols, nil
}

// quote is an SQL identifier.
func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// dumpTable writes one table as a JSON array of row objects, keys sorted,
// rows in primary-key order.
func dumpTable(ctx context.Context, tx *sql.Tx, buf *bytes.Buffer, enc *json.Encoder, table string) error {
	cols, err := columns(ctx, tx, table)
	if err != nil {
		return err
	}
	slices.SortFunc(cols, func(a, b column) int { return strings.Compare(a.name, b.name) })
	var names []string
	var pk []column
	for _, c := range cols {
		names = append(names, quote(c.name))
		if c.pk > 0 {
			pk = append(pk, c)
		}
	}
	slices.SortFunc(pk, func(a, b column) int { return a.pk - b.pk })
	order := []string{"rowid"}
	if len(pk) > 0 {
		order = order[:0]
		for _, c := range pk {
			order = append(order, quote(c.name))
		}
	}
	q := "SELECT " + strings.Join(names, ", ") + " FROM " + quote(table)
	var args []any
	if table == "settings" {
		q += " WHERE key NOT IN (?, ?)"
		args = []any{skipSettings[0], skipSettings[1]}
	}
	q += " ORDER BY " + strings.Join(order, ", ")
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	idCol := slices.IndexFunc(cols, func(c column) bool { return c.name == "id" })
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	buf.WriteByte('[')
	first := true
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteByte('{')
		for i, c := range cols {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeString(buf, enc, c.name); err != nil {
				return err
			}
			buf.WriteByte(':')
			v := vals[i]
			switch {
			case slices.Contains(nulled[table], c.name):
				v = nil
			case table == "attachments" && c.name == "blob_key":
				if v, err = entryKey(vals[idCol]); err != nil {
					return err
				}
			}
			if err := writeValue(buf, enc, v); err != nil {
				return fmt.Errorf("column %s: %w", c.name, err)
			}
		}
		buf.WriteByte('}')
	}
	if err := rows.Err(); err != nil {
		return err
	}
	buf.WriteByte(']')
	return nil
}

// writeValue writes one SQLite value as export.json holds it.
func writeValue(buf *bytes.Buffer, enc *json.Encoder, v any) error {
	switch v := v.(type) {
	case nil:
		buf.WriteString("null")
	case int64:
		buf.WriteString(strconv.FormatInt(v, 10))
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%v has no JSON form", v)
		}
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		buf.WriteString(s)
	case string:
		return writeString(buf, enc, v)
	default:
		return fmt.Errorf("a %T value, which an export can't hold", v)
	}
	return nil
}

// writeString writes s as a JSON string, refusing invalid UTF-8 (which
// encoding/json would quietly replace).
func writeString(buf *bytes.Buffer, enc *json.Encoder, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("text that isn't valid UTF-8")
	}
	if err := enc.Encode(s); err != nil {
		return err
	}
	buf.Truncate(buf.Len() - 1) // Encode's newline
	return nil
}
