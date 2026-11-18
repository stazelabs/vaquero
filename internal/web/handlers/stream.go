package handlers

import (
	"fmt"
	"net/http"
	"time"
)

// streamPing is how long the stream stays silent before a keep-alive comment.
const streamPing = 25 * time.Second

// Stream is GET /events/stream: server-sent events announcing that the
// tracker changed. Every Deps.StreamPoll it reads the newest event sequence
// number and, when it rose, sends "event: change". Pages refresh their
// content on it. The stream clears the server's read and write deadlines
// so it can stay open, and ends when the request does or the server shuts
// down.
func Stream(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Time{})
		_ = rc.SetWriteDeadline(time.Time{})
		last, err := d.R.MaxEventSeq(ctx)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if err := rc.Flush(); err != nil {
			return
		}
		poll := d.StreamPoll
		if poll <= 0 {
			poll = time.Second
		}
		tick := time.NewTicker(poll)
		defer tick.Stop()
		quiet := time.Duration(0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.Shutdown:
				return
			case <-tick.C:
			}
			seq, err := d.R.MaxEventSeq(ctx)
			if err != nil {
				return
			}
			switch {
			case seq > last:
				last, quiet = seq, 0
				_, err = fmt.Fprintf(w, "event: change\ndata: %d\n\n", seq)
			case quiet+poll >= streamPing:
				quiet = 0
				_, err = fmt.Fprint(w, ": ping\n\n")
			default:
				quiet += poll
				continue
			}
			if err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
