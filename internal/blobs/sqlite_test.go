package blobs_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/blobs/blobtest"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

var now = time.Now

func TestSQLite(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blobs.Store {
		d := dbtest.New(t)
		return blobs.NewSQLite(d.W, d.R, now)
	})
}

func TestSQLiteRefusesSizeMismatch(t *testing.T) {
	d := dbtest.New(t)
	s := blobs.NewSQLite(d.W, d.R, now)
	ctx := context.Background()
	if err := s.Put(ctx, "a/b", strings.NewReader("abc"), 4, "text/plain"); err == nil {
		t.Fatal("size mismatch accepted")
	}
	if err := s.Put(ctx, "a/b", strings.NewReader("abcde"), 4, "text/plain"); err == nil {
		t.Fatal("overlong body accepted")
	}
	if _, _, err := s.Get(ctx, "a/b"); err != blobs.ErrNotFound {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
	var n int
	if err := d.R.QueryRow(`SELECT count(*) FROM blobs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows %d, err %v", n, err)
	}
}

func TestSQLiteKeyValidated(t *testing.T) {
	// A closed database makes any SQL fail differently from a key error.
	d := dbtest.New(t)
	s := blobs.NewSQLite(d.W, d.R, now)
	d.W.Close()
	d.R.Close()
	ctx := context.Background()
	err := s.Put(ctx, "../x", strings.NewReader("x"), 1, "text/plain")
	if err == nil || !strings.Contains(err.Error(), "blobs:") {
		t.Fatalf("Put = %v, want a key error", err)
	}
	if _, _, err := s.Get(ctx, "../x"); err == nil || !strings.Contains(err.Error(), "blobs:") {
		t.Fatalf("Get = %v", err)
	}
	if err := s.Delete(ctx, "../x"); err == nil || !strings.Contains(err.Error(), "blobs:") {
		t.Fatalf("Delete = %v", err)
	}
}

func TestSQLiteListOrdersByKey(t *testing.T) {
	d := dbtest.New(t)
	s := blobs.NewSQLite(d.W, d.R, now)
	ctx := context.Background()
	for _, k := range []string{"a/c", "a/a", "a/b", "ab/z"} {
		if err := s.Put(ctx, k, strings.NewReader("x"), 1, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := s.List(ctx, "a", func(k string, _ time.Time) error { got = append(got, k); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a/a,a/b,a/c" {
		t.Fatalf("got %v", got)
	}
}
