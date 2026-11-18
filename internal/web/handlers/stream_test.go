package handlers_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// openStream connects to /events/stream on base and returns a channel of
// the lines it sends, closed when the stream ends.
func openStream(t *testing.T, base string) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/events/stream", nil)
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
