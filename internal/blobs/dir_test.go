package blobs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/blobs/blobtest"
)

func TestDir(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blobs.Store {
		d, err := blobs.NewDir(filepath.Join(t.TempDir(), "blobs"))
		if err != nil {
			t.Fatal(err)
		}
		return d
	})
}

func TestDirLeftoverTempFileIsInvisible(t *testing.T) {
	root := t.TempDir()
	d, err := blobs.NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(root, "images"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A crash between create and rename leaves a dot-prefixed temp file.
	if err := os.WriteFile(filepath.Join(root, "images", ".tmp-a.png-123"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(ctx, "images/b.png", strings.NewReader("ok"), 2, "image/png"); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := d.List(ctx, "images/", func(k string, _ time.Time) error { keys = append(keys, k); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "images/b.png" {
		t.Fatalf("keys %v", keys)
	}
	if _, _, err := d.Get(ctx, "images/a.png"); err == nil {
		t.Fatal("partial write was readable")
	}
	if blobs.ValidKey("images/.tmp-a.png-123") == nil {
		t.Fatal("temp name must not be a valid key")
	}
}

func TestDirPutLeavesNoTempOnSizeMismatch(t *testing.T) {
	root := t.TempDir()
	d, _ := blobs.NewDir(root)
	err := d.Put(context.Background(), "images/a.png", strings.NewReader("abc"), 10, "image/png")
	if err == nil {
		t.Fatal("size mismatch accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "images"))
	if len(entries) != 0 {
		t.Fatalf("left behind %v", entries)
	}
}

func TestDirRootThatIsAFileIsRefused(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.NewDir(f); err == nil {
		t.Fatal("file root accepted")
	}
}

func TestDirRootCreatedPrivate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b")
	if _, err := blobs.NewDir(root); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		t.Fatalf("stat %v %v", st, err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestDirCancelledContext(t *testing.T) {
	d, _ := blobs.NewDir(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Put(ctx, "a", strings.NewReader("x"), 1, "x/y"); err == nil {
		t.Fatal("Put with cancelled ctx succeeded")
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }

func TestDirErrorPaths(t *testing.T) {
	if _, err := blobs.NewDir(""); err == nil {
		t.Fatal("empty root accepted")
	}
	root := t.TempDir()
	d, _ := blobs.NewDir(root)
	ctx := context.Background()
	dead, cancel := context.WithCancel(ctx)
	cancel()

	if _, _, err := d.Get(dead, "a"); err == nil {
		t.Error("Get with cancelled ctx")
	}
	if err := d.Delete(dead, "a"); err == nil {
		t.Error("Delete with cancelled ctx")
	}
	if err := d.List(dead, "", func(string, time.Time) error { return nil }); err == nil {
		t.Error("List with cancelled ctx")
	}

	// A reader that fails leaves nothing behind.
	if err := d.Put(ctx, "images/a.png", failReader{}, 1, "x/y"); err == nil {
		t.Error("failing reader accepted")
	}
	if ents, _ := os.ReadDir(filepath.Join(root, "images")); len(ents) != 0 {
		t.Errorf("left behind %v", ents)
	}

	// A file where a directory is needed.
	if err := d.Put(ctx, "images/a.png", strings.NewReader("x"), 1, "x/y"); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(ctx, "images/a.png/b", strings.NewReader("x"), 1, "x/y"); err == nil {
		t.Error("Put under a file accepted")
	}
	if _, _, err := d.Get(ctx, "images/a.png/b"); err == nil {
		t.Error("Get under a file succeeded")
	} else if errors.Is(err, blobs.ErrNotFound) {
		t.Log("not found under a file (platform)")
	}

	// Delete of a prefix is a no-op and keeps its objects.
	if err := d.Delete(ctx, "images"); err != nil {
		t.Errorf("Delete of a prefix: %v", err)
	}
	if _, _, err := d.Get(ctx, "images/a.png"); err != nil {
		t.Errorf("object lost: %v", err)
	}

	// Listing a prefix that is a file, and a cancelled walk.
	n := 0
	if err := d.List(ctx, "images/a.png", func(string, time.Time) error { n++; return nil }); err != nil || n != 0 {
		t.Errorf("List of a file: %v, %d", err, n)
	}
	if err := d.List(dead, "images/", func(string, time.Time) error { return nil }); err == nil {
		t.Error("List with cancelled ctx")
	}
	if err := d.List(ctx, strings.Repeat("a", blobs.MaxKeyLen+2), func(string, time.Time) error { return nil }); err == nil {
		t.Error("overlong prefix accepted")
	}
}

// TestDirListSkipsObjectsDeletedDuringTheWalk: an object removed while List
// runs (an attachment removed mid-walk) is skipped, not an error.
func TestDirListSkipsObjectsDeletedDuringTheWalk(t *testing.T) {
	ctx := context.Background()
	d, err := blobs.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"images/a.png", "images/b.png", "images/c.png"} {
		if err := d.Put(ctx, k, strings.NewReader("x"), 1, "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	err = d.List(ctx, "images", func(key string, _ time.Time) error {
		seen = append(seen, key)
		if len(seen) == 1 {
			for _, k := range []string{"images/a.png", "images/b.png", "images/c.png"} {
				if k != key {
					if err := d.Delete(ctx, k); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		return nil
	})
	if err != nil || len(seen) != 1 {
		t.Fatalf("List = %v, seen %v", err, seen)
	}
}
