// Package blobs stores attachment bytes behind a small Store interface, with
// three backends: SQLite (the default, in the database file itself), Dir (a
// directory) and S3 (an S3-compatible bucket). It is pure except for the
// backends' own storage.
//
// Key scheme: slash-separated paths of lowercase segments, minted by the app
// as attachments/{instance}/{attachment id}. Keys are never user text.
// ValidKey is the only rule: non-empty segments of [a-z0-9._-], no segment
// that starts with a dot (so "." and ".." are impossible and a backend's
// dot-prefixed temp files can never collide with a key), at most MaxKeyLen
// bytes in all. Every backend calls ValidKey first and returns its error
// before touching storage, so a key cannot traverse out of a backend's root.
// A List prefix is "" or a directory path (one trailing slash allowed) made
// of the same segments.
//
// Deadline rule: PutDeadline is the longest a caller should let a Put run.
// Callers wrap their own context with it; backends do not add a deadline.
//
// Orphans: a database row and its object are not written atomically, even
// in the SQLite backend (its Put and core's row are separate transactions).
// An upload puts the bytes first, then core writes the attachments row; if
// core refuses or fails, the handler deletes the object. A delete removes
// the row first, then the handler deletes the object. Whatever still slips
// through (a crash in between, a failed delete) is an orphan: the sweep
// lists attachments/{instance}/ and deletes objects with no row that are
// older than an hour. Dir's interrupted writes leave dot-prefixed temp
// files that List and Get never show.
//
// Backends are tested with blobtest.Run.
package blobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrNotFound is returned by Get when the key is absent.
var ErrNotFound = errors.New("blobs: not found")

// PutDeadline is how long a caller should allow a Put.
const PutDeadline = 10 * time.Second

// MaxKeyLen is the longest valid key, in bytes.
const MaxKeyLen = 200

// Store is an object store addressed by app-minted keys.
type Store interface {
	// Put stores size bytes from r under key, replacing any object there.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Get opens the object and reports its size. ErrNotFound when absent.
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// Delete removes the object. An absent key is not an error.
	Delete(ctx context.Context, key string) error
	// List calls fn for each object under the directory prefix with its
	// modified time, stopping at fn's first error.
	List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error
}

// ValidKey reports why key is not a legal key, or nil.
func ValidKey(key string) error {
	if key == "" {
		return errors.New("blobs: empty key")
	}
	if len(key) > MaxKeyLen {
		return fmt.Errorf("blobs: key longer than %d bytes", MaxKeyLen)
	}
	return validSegments(key)
}

// validPrefix accepts "" or a directory path with one optional trailing slash.
func validPrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	dir := strings.TrimSuffix(prefix, "/")
	if len(dir) > MaxKeyLen {
		return "", fmt.Errorf("blobs: prefix longer than %d bytes", MaxKeyLen)
	}
	if err := validSegments(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func validSegments(s string) error {
	for seg := range strings.SplitSeq(s, "/") {
		if seg == "" {
			return errors.New("blobs: empty path segment")
		}
		if seg[0] == '.' {
			return errors.New("blobs: segment starts with a dot")
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return fmt.Errorf("blobs: character %q not allowed", c)
			}
		}
	}
	return nil
}
