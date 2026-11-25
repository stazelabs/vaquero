// Package blobtest is the conformance suite every blobs.Store backend must
// pass. A backend's tests call Run with a function that opens a fresh, empty
// store.
package blobtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
)

// InvalidKeys are shapes ValidKey must refuse; every backend method refuses
// them too.
func InvalidKeys() []string {
	return []string{
		"", "/", "/a", "a/", "a//b", ".", "..", "./a", "a/./b", "a/../b", "../a",
		"A", "images/ABC.png", "a b", "a\\b", "a/.hidden", ".tmp-1", "a\x00b",
		"é", "a?b", "a:b", "a*b", strings.Repeat("a", blobs.MaxKeyLen+1),
	}
}

// Run runs the suite as subtests. open must return a fresh empty store.
func Run(t *testing.T, open func(t *testing.T) blobs.Store) {
	ctx := context.Background()
	put := func(t *testing.T, s blobs.Store, key, body string) {
		t.Helper()
		if err := s.Put(ctx, key, strings.NewReader(body), int64(len(body)), "application/octet-stream"); err != nil {
			t.Fatalf("Put(%q): %v", key, err)
		}
	}
	read := func(t *testing.T, s blobs.Store, key string) (string, int64) {
		t.Helper()
		rc, size, err := s.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b), size
	}

	t.Run("round trip", func(t *testing.T) {
		s := open(t)
		put(t, s, "images/a.png", "hello")
		got, size := read(t, s, "images/a.png")
		if got != "hello" || size != 5 {
			t.Fatalf("got %q size %d", got, size)
		}
	})
	t.Run("binary and empty", func(t *testing.T) {
		s := open(t)
		want := bytes.Repeat([]byte{0, 1, 2, 255}, 5000)
		if err := s.Put(ctx, "mail/x.gz", bytes.NewReader(want), int64(len(want)), "application/gzip"); err != nil {
			t.Fatal(err)
		}
		got, size := read(t, s, "mail/x.gz")
		if got != string(want) || size != int64(len(want)) {
			t.Fatalf("binary mismatch, size %d", size)
		}
		put(t, s, "mail/empty.gz", "")
		if got, size := read(t, s, "mail/empty.gz"); got != "" || size != 0 {
			t.Fatalf("empty: %q %d", got, size)
		}
	})
	t.Run("overwrite replaces", func(t *testing.T) {
		s := open(t)
		put(t, s, "images/a.png", "long first body")
		put(t, s, "images/a.png", "two")
		if got, size := read(t, s, "images/a.png"); got != "two" || size != 3 {
			t.Fatalf("got %q size %d", got, size)
		}
	})
	t.Run("get missing is ErrNotFound", func(t *testing.T) {
		s := open(t)
		for _, key := range []string{"images/none.png", "none", "deep/er/none.bin"} {
			if _, _, err := s.Get(ctx, key); !errors.Is(err, blobs.ErrNotFound) {
				t.Fatalf("Get(%q) = %v, want ErrNotFound", key, err)
			}
		}
	})
	t.Run("get of a prefix is ErrNotFound", func(t *testing.T) {
		s := open(t)
		put(t, s, "images/a.png", "x")
		if _, _, err := s.Get(ctx, "images"); !errors.Is(err, blobs.ErrNotFound) {
			t.Fatalf("Get(images) = %v, want ErrNotFound", err)
		}
	})
	t.Run("delete twice is fine", func(t *testing.T) {
		s := open(t)
		put(t, s, "images/a.png", "x")
		for range 2 {
			if err := s.Delete(ctx, "images/a.png"); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}
		if _, _, err := s.Get(ctx, "images/a.png"); !errors.Is(err, blobs.ErrNotFound) {
			t.Fatalf("Get after delete = %v", err)
		}
		if err := s.Delete(ctx, "never/existed.bin"); err != nil {
			t.Fatalf("Delete of absent: %v", err)
		}
	})
	t.Run("list by prefix", func(t *testing.T) {
		s := open(t)
		start := time.Now().Add(-time.Minute)
		put(t, s, "images/a.png", "1")
		put(t, s, "images/b.png", "2")
		put(t, s, "mail/c.gz", "3")
		list := func(prefix string) []string {
			t.Helper()
			var keys []string
			err := s.List(ctx, prefix, func(key string, modified time.Time) error {
				if modified.Before(start) || modified.After(time.Now().Add(time.Minute)) {
					t.Errorf("%q modified %v out of range", key, modified)
				}
				keys = append(keys, key)
				return nil
			})
			if err != nil {
				t.Fatalf("List(%q): %v", prefix, err)
			}
			slices.Sort(keys)
			return keys
		}
		if got := list("images/"); !slices.Equal(got, []string{"images/a.png", "images/b.png"}) {
			t.Fatalf("images/: %v", got)
		}
		if got := list("images"); !slices.Equal(got, []string{"images/a.png", "images/b.png"}) {
			t.Fatalf("images: %v", got)
		}
		if got := list("mail/"); !slices.Equal(got, []string{"mail/c.gz"}) {
			t.Fatalf("mail/: %v", got)
		}
		if got := list(""); len(got) != 3 {
			t.Fatalf("all: %v", got)
		}
		if got := list("nothing/"); len(got) != 0 {
			t.Fatalf("nothing/: %v", got)
		}
	})
	t.Run("list stops on callback error", func(t *testing.T) {
		s := open(t)
		put(t, s, "images/a.png", "1")
		put(t, s, "images/b.png", "2")
		stop := errors.New("stop")
		n := 0
		err := s.List(ctx, "images/", func(string, time.Time) error { n++; return stop })
		if !errors.Is(err, stop) || n != 1 {
			t.Fatalf("err %v after %d calls", err, n)
		}
	})
	t.Run("invalid keys are refused", func(t *testing.T) {
		s := open(t)
		for _, key := range InvalidKeys() {
			if err := blobs.ValidKey(key); err == nil {
				t.Errorf("ValidKey(%q) accepted", key)
			}
			if err := s.Put(ctx, key, strings.NewReader("x"), 1, "x/y"); err == nil {
				t.Errorf("Put(%q) accepted", key)
			}
			if _, _, err := s.Get(ctx, key); err == nil || errors.Is(err, blobs.ErrNotFound) {
				t.Errorf("Get(%q) = %v, want a key error", key, err)
			}
			if err := s.Delete(ctx, key); err == nil {
				t.Errorf("Delete(%q) accepted", key)
			}
		}
		for _, prefix := range []string{"/", "//", "../x", "a/../b", "A/", "a b/", "/a", "a//", ".hidden/"} {
			err := s.List(ctx, prefix, func(string, time.Time) error { return nil })
			if err == nil {
				t.Errorf("List(%q) accepted", prefix)
			}
		}
	})
}
