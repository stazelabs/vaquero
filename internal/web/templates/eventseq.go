package templates

import (
	"context"
	"strconv"
	"sync"
)

type eventSeqKey struct{}

// WithEventSeq records the newest event sequence as of the start of the
// request; Shell writes it as the body's data-event-seq, and app.js opens
// the live stream from it (?since=), so a change made while the page was
// being built or before the stream connected is still announced.
func WithEventSeq(ctx context.Context, seq int64) context.Context {
	return context.WithValue(ctx, eventSeqKey{}, func() (int64, bool) { return seq, true })
}

// WithEventSeqFunc is WithEventSeq read on first use: load runs at most once,
// when LoadEventSeq or a page's Shell first asks, and not at all for a
// request that never renders a page (a form POST that redirects). ok false
// from load means no sequence (Shell leaves the attribute out).
func WithEventSeqFunc(ctx context.Context, load func() (int64, bool)) context.Context {
	return context.WithValue(ctx, eventSeqKey{}, sync.OnceValues(load))
}

// LoadEventSeq reads a lazily recorded sequence now, if there is one and it
// isn't read yet. A handler about to re-read a page's data calls it first,
// so the sequence is no newer than what the page shows.
func LoadEventSeq(ctx context.Context) {
	if f, ok := ctx.Value(eventSeqKey{}).(func() (int64, bool)); ok {
		f()
	}
}

// eventSeq is the recorded sequence, as text; ok is false when none was.
func eventSeq(ctx context.Context) (string, bool) {
	f, ok := ctx.Value(eventSeqKey{}).(func() (int64, bool))
	if !ok {
		return "", false
	}
	n, ok := f()
	if !ok {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}
