package blobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// tempPrefix starts every in-flight file name. ValidKey refuses segments that
// start with a dot, so no key can collide with a temp file and List skips them.
const tempPrefix = ".tmp-"

// Dir is a Store on the local filesystem under Root.
type Dir struct{ Root string }

// NewDir creates root (mode 0o700) if needed and refuses a root that exists
// and is not a directory.
func NewDir(root string) (*Dir, error) {
	if root == "" {
		return nil, errors.New("blobs: empty root")
	}
	if st, err := os.Stat(root); err == nil && !st.IsDir() {
		return nil, fmt.Errorf("blobs: root %s is not a directory", root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("blobs: create root: %w", err)
	}
	return &Dir{Root: root}, nil
}

func (d *Dir) path(key string) string { return filepath.Join(d.Root, filepath.FromSlash(key)) }

// Put writes to a temp file beside the key, fsyncs, then renames over it.
func (d *Dir) Put(ctx context.Context, key string, r io.Reader, size int64, _ string) (err error) {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	final := d.path(key)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, tempPrefix+path.Base(key)+"-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	n, err := io.Copy(f, r)
	if err != nil {
		return err
	}
	if size >= 0 && n != size {
		return fmt.Errorf("blobs: wrote %d bytes, expected %d", n, size)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Get opens the object; a missing key or a directory is ErrNotFound.
func (d *Dir) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := ValidKey(key); err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	f, err := os.Open(d.path(key))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	if st.IsDir() {
		_ = f.Close()
		return nil, 0, ErrNotFound
	}
	return f, st.Size(), nil
}

// Delete removes the object; absent is fine.
func (d *Dir) Delete(ctx context.Context, key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(d.path(key))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// A directory (a prefix, not an object) or a file as a parent
		// directory is absent as far as callers are concerned.
		if st, serr := os.Stat(d.path(key)); serr == nil && st.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// List walks the prefix directory, skipping temp files.
func (d *Dir) List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error {
	dir, err := validPrefix(prefix)
	if err != nil {
		return err
	}
	start := d.Root
	if dir != "" {
		start = d.path(dir)
	}
	if st, err := os.Stat(start); err != nil || !st.IsDir() {
		return nil // nothing under that prefix
	}
	return filepath.WalkDir(start, func(p string, e fs.DirEntry, err error) error {
		// An object deleted while the walk runs (a replaced avatar) is
		// simply no longer listed.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		if strings.HasPrefix(e.Name(), ".") || !e.Type().IsRegular() {
			return nil
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), info.ModTime())
	})
}
