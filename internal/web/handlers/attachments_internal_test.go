package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// storeUpload puts the bytes before core writes the row; when core refuses,
// the bytes are deleted again.
func TestStoreUploadRefusedDeletesBytes(t *testing.T) {
	db := dbtest.New(t)
	now := func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	c := core.New(db.W, now)
	owner, err := c.EnsureOwner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{Core: c, Blobs: blobs.NewSQLite(db.W, db.R, now), Instance: "inst", Logger: slog.New(slog.DiscardHandler), Now: now}
	_, err = storeUpload(context.Background(), d, owner.ID, "01NOSUCHTICKET", bytes.NewReader([]byte("hello")), "a.txt")
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	n := 0
	if err := d.Blobs.List(context.Background(), "", func(string, time.Time) error { n++; return nil }); err != nil || n != 0 {
		t.Fatalf("%d objects left, %v", n, err)
	}
}

func TestAttachKey(t *testing.T) {
	if got := attachKey("inst", "01HABCDEF"); got != "attachments/inst/01habcdef" {
		t.Fatal(got)
	}
}
