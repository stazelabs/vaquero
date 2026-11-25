package handlers

import (
	"context"
	"sync/atomic"
	"testing"
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
