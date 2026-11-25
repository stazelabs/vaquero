package handlers_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/handlers"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// openStream connects to /events/stream on base and returns a channel of
// the lines it sends, closed when the stream ends.
func openStream(t *testing.T, base string) (<-chan string, context.CancelFunc) {
	t.Helper()
	return openStreamAt(t, base, "/events/stream", "")
}

// openStreamAt is openStream on path, sending lastID as Last-Event-ID
// when it is set, the way a browser's EventSource reconnects.
func openStreamAt(t *testing.T, base, path, lastID string) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+path, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		cancel()
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	t.Cleanup(cancel)
	return lines, cancel
}

func waitFor(t *testing.T, lines <-chan string, want string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream ended before %q", want)
			}
			if l == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q within %v", want, within)
		}
	}
}

func TestStreamAnnouncesChanges(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	lines, _ := openStream(t, e.Server.URL)
	time.Sleep(50 * time.Millisecond)
	ticket(t, e, p, core.TicketInput{Title: "new"})
	waitFor(t, lines, "event: change", time.Second)
}

// Review Focus 5: the stream outlives the server's read and write timeouts.
func TestStreamOutlivesTimeouts(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	srv := httptest.NewUnstartedServer(web.NewRouter(e.Deps))
	srv.Config.ReadTimeout = 200 * time.Millisecond
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	lines, _ := openStream(t, srv.URL)
	time.Sleep(600 * time.Millisecond)
	ticket(t, e, p, core.TicketInput{Title: "late"})
	waitFor(t, lines, "event: change", time.Second)
}

// Review Focus 4: shutdown ends open streams at once.
func TestStreamStopsOnShutdown(t *testing.T) {
	e := webtest.New(t)
	lines, _ := openStream(t, e.Server.URL)
	time.Sleep(50 * time.Millisecond)
	e.Shutdown()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream still open after shutdown")
		}
	}
}

func TestPagesSubscribeToStream(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	for path, target := range map[string]string{
		"/p/VAQ":      `hx-get="/p/VAQ/board"`,
		"/p/VAQ/list": `hx-select="#list"`,
		"/activity":   `hx-select="#feed"`,
	} {
		page := c.Get(path).Body
		// app.js owns the one stream per tab and triggers vq:change on
		// every [data-live] part; no htmx extension holds a connection.
		contains(t, page, "data-live", `hx-trigger="vq:change"`, target)
		lacks(t, page, "sse-connect", "htmx-ext-sse")
	}
}

// Open rails are tagged so app.js can keep them open across refreshes.
func TestRailsCarryStableIDs(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	page := e.Client().Get("/p/VAQ").Body
	if n := strings.Count(page, "data-rail="); n != 2 {
		t.Fatalf("%d rails tagged", n)
	}
	contains(t, page, "data-board-scroll")
}

// An MCP call is announced as "mcp", not "change": only the MCP activity
// page follows it, so an agent's reads don't redraw every board.
func TestStreamAnnouncesMCPCalls(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	lines, _ := openStream(t, e.Server.URL)
	time.Sleep(50 * time.Millisecond)
	mcpCall(t, cs, "list_projects", nil)
	waitFor(t, lines, "event: mcp", time.Second)
}

// seqNow is the newest event sequence number.
func seqNow(t *testing.T, e *webtest.Env) int64 {
	t.Helper()
	n, err := e.Deps.R.MaxEventSeq(bg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// quiet fails if the stream sends want within d.
func quiet(t *testing.T, lines <-chan string, want string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return
			}
			if l == want {
				t.Fatalf("unexpected %q", want)
			}
		case <-deadline:
			return
		}
	}
}

// VAQ-36 (Review Focus 4): each change carries its sequence as the event id.
func TestStreamChangesCarryTheirSequence(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	lines, _ := openStream(t, e.Server.URL)
	time.Sleep(50 * time.Millisecond)
	ticket(t, e, p, core.TicketInput{Title: "new"})
	waitFor(t, lines, "id: "+strconv.FormatInt(seqNow(t, e), 10), time.Second)
}

// A page carries the sequence it was rendered at; a stream opened with it
// announces a change made in between at once, and one opened at the
// current sequence stays quiet (no duplicate refresh).
func TestStreamCatchesUpFromThePage(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	page := e.Client().Get("/p/VAQ").Body
	m := regexp.MustCompile(`data-event-seq="(\d+)"`).FindStringSubmatch(page)
	if m == nil || m[1] != strconv.FormatInt(seqNow(t, e), 10) {
		t.Fatalf("page sequence %v, newest %d", m, seqNow(t, e))
	}
	ticket(t, e, p, core.TicketInput{Title: "between render and connect"})
	lines, _ := openStreamAt(t, e.Server.URL, "/events/stream?since="+m[1], "")
	waitFor(t, lines, "event: change", 200*time.Millisecond)

	current, _ := openStreamAt(t, e.Server.URL, "/events/stream?since="+strconv.FormatInt(seqNow(t, e), 10), "")
	quiet(t, current, "event: change", 200*time.Millisecond)
}

// After a restart the browser reconnects with Last-Event-ID, which wins
// over the page's since: changes made while it was away are announced once.
func TestStreamHonoursLastEventID(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	before := strconv.FormatInt(seqNow(t, e), 10)
	ticket(t, e, p, core.TicketInput{Title: "while disconnected"})
	now := strconv.FormatInt(seqNow(t, e), 10)

	lines, _ := openStreamAt(t, e.Server.URL, "/events/stream?since="+now, before)
	waitFor(t, lines, "id: "+now, 200*time.Millisecond)
	waitFor(t, lines, "event: change", 200*time.Millisecond)
	quiet(t, lines, "event: change", 200*time.Millisecond)

	// Up to date: only the id, so the browser's next reconnect starts there.
	same, _ := openStreamAt(t, e.Server.URL, "/events/stream?since="+before, now)
	waitFor(t, same, "id: "+now, 200*time.Millisecond)
	quiet(t, same, "event: change", 200*time.Millisecond)
}

// Pages and re-rendered forms carry the sequence; a redirecting POST and
// an htmx swap don't need it and aren't given it.
func TestPageSequenceOnlyOnPages(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	contains(t, c.Get("/p/VAQ/settings").Body, "data-event-seq=")
	contains(t, c.Post("/p/VAQ/statuses", url.Values{"name": {"todo"}, "category": {"todo"}}).Body, "data-event-seq=")
	contains(t, c.Post("/admin/tokens", url.Values{"name": {"ci"}}).Body, "data-event-seq=")
	lacks(t, c.GetHX("/p/VAQ/list").Body, "data-event-seq=")
}

// A form POST reads the event sequence only when it re-renders its page:
// a save that redirects never needs it.
func TestFormSequenceReadOnlyOnRerender(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	reads := handlers.CountSeqReads(t)
	if r := c.Post("/p/VAQ/statuses", url.Values{"name": {"Waiting"}, "category": {"todo"}}); r.Status != http.StatusSeeOther {
		t.Fatalf("save: %d", r.Status)
	}
	if n := reads.Load(); n != 0 {
		t.Fatalf("a redirecting save read the sequence %d times", n)
	}
	r := c.Post("/p/VAQ/statuses", url.Values{"name": {"todo"}, "category": {"todo"}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("refusal: %d", r.Status)
	}
	contains(t, r.Body, `data-event-seq="`+strconv.FormatInt(seqNow(t, e), 10)+`"`)
	if n := reads.Load(); n != 1 {
		t.Fatalf("a re-rendered form read the sequence %d times, want 1", n)
	}
	reads.Store(0)
	contains(t, c.Get("/p/VAQ/settings").Body, "data-event-seq=")
	if n := reads.Load(); n != 1 {
		t.Fatalf("a page read the sequence %d times, want 1", n)
	}
}

// A quiet stream sends a keep-alive comment, so proxies and browsers don't
// drop it.
func TestStreamPingsWhenQuiet(t *testing.T) {
	handlers.SetStreamPing(t, 100*time.Millisecond)
	e := webtest.New(t)
	lines, _ := openStream(t, e.Server.URL)
	waitFor(t, lines, ": ping", 2*time.Second)
}

// A re-rendered form reads the sequence before the page's data, so a change
// made in between is shown by the page and still announced by the stream.
func TestFormRerenderReadsSequenceBeforeData(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Anchor"})
	c := e.Client()
	before := seqNow(t, e)
	handlers.AfterSeqRead(t, func() {
		if _, err := e.Deps.Core.AddComment(bg, e.Deps.Actor.ID, tk.ID, "Added between"); err != nil {
			t.Error(err)
		}
	})
	r := c.Post("/t/VAQ-1/links", url.Values{"kind": {"blocks"}, "to": {"nonsense"}})
	if r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("refusal: %d", r.Status)
	}
	contains(t, r.Body, "Added between", `data-event-seq="`+strconv.FormatInt(before, 10)+`"`)
}
