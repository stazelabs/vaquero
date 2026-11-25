package templates

import (
	"context"
	"strconv"
)

type eventSeqKey struct{}

// WithEventSeq records the newest event sequence as of the start of the
// request; Shell writes it as the body's data-event-seq, and app.js opens
// the live stream from it (?since=), so a change made while the page was
// being built or before the stream connected is still announced.
func WithEventSeq(ctx context.Context, seq int64) context.Context {
	return context.WithValue(ctx, eventSeqKey{}, seq)
}

// eventSeq is the recorded sequence, as text; ok is false when none was.
func eventSeq(ctx context.Context) (string, bool) {
	n, ok := ctx.Value(eventSeqKey{}).(int64)
	if !ok {
		return "", false
	}
	return strconv.FormatInt(n, 10), true
}
