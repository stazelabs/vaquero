package handlers

import (
	"context"
	"net/http"
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

// CancelBulkAt ends a bulk call's request just before its item i runs.
func CancelBulkAt(t testing.TB, i int) {
	t.Helper()
	saved := bulkStep
	bulkStep = func(r *http.Request, n int) *http.Request {
		if n == i {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			return r.WithContext(ctx)
		}
		return r
	}
	t.Cleanup(func() { bulkStep = saved })
}
