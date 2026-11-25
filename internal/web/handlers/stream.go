package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/stazelabs/vaquero/internal/web/templates"
)

// streamPing is how long the stream stays silent before a keep-alive comment.
const streamPing = 25 * time.Second

// Stream is GET /events/stream: server-sent events announcing that the
// tracker changed. Every Deps.StreamPoll it reads the newest event sequence
// number and, when it rose, sends "event: change" with the sequence as its
// id. Pages refresh their content on it. When a new MCP call was logged it
// sends "event: mcp", which only the MCP activity page follows.
//
// A stream starts from the sequence its client last saw: the Last-Event-ID
// header (a browser reconnecting, after a restart say), else ?since= (the
// sequence the page was rendered at, data-event-seq), else now. If the
// newest sequence differs from it, a change is announced at once, so
// nothing made between a render and its stream, or while the server was
// down, goes unannounced; otherwise only the id is sent, so the browser's
// next reconnect starts from it.
//
// The stream clears the server's read and write deadlines so it can stay
// open, and ends when the request does or the server shuts down.
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
		seen, known := streamSince(r)
		lastCall, err := d.R.NewestMCPCall(ctx)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if known && seen != last {
			_, err = fmt.Fprintf(w, "id: %d\nevent: change\ndata: %d\n\n", last, last)
		} else {
			_, err = fmt.Fprintf(w, "id: %d\n\n", last)
		}
		if err != nil || rc.Flush() != nil {
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
			call, err := d.R.NewestMCPCall(ctx)
			if err != nil {
				return
			}
			switch {
			case seq > last:
				last, quiet = seq, 0
				_, err = fmt.Fprintf(w, "id: %d\nevent: change\ndata: %d\n\n", seq, seq)
				if err == nil && call > lastCall {
					lastCall = call
					_, err = fmt.Fprint(w, "event: mcp\ndata: 1\n\n")
				}
			case call > lastCall:
				lastCall, quiet = call, 0
				_, err = fmt.Fprint(w, "event: mcp\ndata: 1\n\n")
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

// streamSince is the event sequence the client last saw: Last-Event-ID,
// else ?since=. known is false when it sent neither (or nonsense).
func streamSince(r *http.Request) (seq int64, known bool) {
	for _, v := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("since")} {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}

// PageSeq records the newest event sequence before a page handler reads
// anything (templates.WithEventSeq), so the page's live stream can catch up
// on changes made after this point. htmx requests swap parts of a page that
// already has it, and are left alone.
func PageSeq(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("HX-Request") != "true" {
				if seq, err := d.R.MaxEventSeq(r.Context()); err == nil {
					r = r.WithContext(templates.WithEventSeq(r.Context(), seq))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
