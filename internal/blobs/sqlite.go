package blobs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// SQLite is a Store in the blobs table of the application database. It
// writes through the single-connection writer pool and reads through the
// reader pool. Each Put holds the whole object in memory, which is fine for
// the 10 MB attachment limit.
type SQLite struct {
	w, r *sql.DB
	now  func() time.Time
}

// NewSQLite makes a store writing through w and reading through r, stamping
// objects with now.
func NewSQLite(w, r *sql.DB, now func() time.Time) *SQLite {
	return &SQLite{w: w, r: r, now: now}
}

// Put reads the whole body (at most size+1 bytes, so an overlong one is
// caught), refuses a size mismatch, then upserts.
func (s *SQLite) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if size < 0 {
		return errors.New("blobs: sqlite needs the size up front")
	}
	b, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return fmt.Errorf("blobs: read %d bytes, expected %d", len(b), size)
	}
	return gen.New(s.w).PutBlob(ctx, gen.PutBlobParams{
		Key: key, Bytes: b, ContentType: contentType, Size: size, Modified: timefmt.Format(s.now()),
	})
}

// Get returns the object; a missing key is ErrNotFound.
func (s *SQLite) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := ValidKey(key); err != nil {
		return nil, 0, err
	}
	row, err := gen.New(s.r).GetBlob(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	return io.NopCloser(bytes.NewReader(row.Bytes)), row.Size, nil
}

// Stat reads the object's size, not its bytes; a missing key is ErrNotFound.
func (s *SQLite) Stat(ctx context.Context, key string) (int64, error) {
	if err := ValidKey(key); err != nil {
		return 0, err
	}
	size, err := gen.New(s.r).StatBlob(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return size, err
}

// Delete removes the object; absent is fine.
func (s *SQLite) Delete(ctx context.Context, key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	return gen.New(s.w).DeleteBlob(ctx, key)
}

// List calls fn for each object under the directory prefix, in key order.
func (s *SQLite) List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error {
	dir, err := validPrefix(prefix)
	if err != nil {
		return err
	}
	if dir != "" {
		dir += "/"
	}
	rows, err := gen.New(s.r).ListBlobs(ctx, dir)
	if err != nil {
		return err
	}
	for _, row := range rows {
		mod, err := timefmt.Parse(row.Modified)
		if err != nil {
			return err
		}
		if err := fn(row.Key, mod); err != nil {
			return err
		}
	}
	return nil
}
