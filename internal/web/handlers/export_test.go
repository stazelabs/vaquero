package handlers

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// CountSeqReads counts the newest-event-sequence reads the page middleware
// makes for one test; tests that call it must not run in parallel.
func CountSeqReads(t testing.TB) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	saved := readEventSeq
	readEventSeq = func(ctx context.Context, d Deps) (int64, error) {
		n.Add(1)
		return saved(ctx, d)
	}
	t.Cleanup(func() { readEventSeq = saved })
	return &n
}

// SetStreamPing shortens the stream's keep-alive interval for one test.
func SetStreamPing(t testing.TB, d time.Duration) {
	t.Helper()
	saved := streamPing
	streamPing = d
	t.Cleanup(func() { streamPing = saved })
}

// AfterSeqRead runs fn once, right after the first event-sequence read of a
// test; tests that call it must not run in parallel.
func AfterSeqRead(t testing.TB, fn func()) {
	t.Helper()
	var once sync.Once
	saved := readEventSeq
	readEventSeq = func(ctx context.Context, d Deps) (int64, error) {
		n, err := saved(ctx, d)
		once.Do(fn)
		return n, err
	}
	t.Cleanup(func() { readEventSeq = saved })
}

// OnBulkStep runs fn before bulk item i's action (action "" for the item
// itself), with the call's cancel; tests that call it must not run in
// parallel.
func OnBulkStep(t testing.TB, i int, action string, fn func(cancel context.CancelFunc)) {
	t.Helper()
	saved := bulkStep
	bulkStep = func(n int, a string, cancel context.CancelFunc) {
		if n == i && a == action {
			fn(cancel)
		}
	}
	t.Cleanup(func() { bulkStep = saved })
}

// CancelBulkAt ends a bulk call's request just before its item i runs.
func CancelBulkAt(t testing.TB, i int) {
	t.Helper()
	OnBulkStep(t, i, "", func(cancel context.CancelFunc) { cancel() })
}

// SetPreviewMaker replaces thumbs.Make and its timeout for one test.
func SetPreviewMaker(t testing.TB, timeout time.Duration, fn func(context.Context, io.Reader) ([]byte, string, error)) {
	t.Helper()
	savedFn, savedTimeout := makePreview, thumbTimeout
	makePreview, thumbTimeout = fn, timeout
	t.Cleanup(func() { makePreview, thumbTimeout = savedFn, savedTimeout })
}
