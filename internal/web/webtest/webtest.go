// Package webtest boots the real router on a test database for handler
// tests: a fixed clock, a cookie-free client that never follows redirects.
package webtest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/handlers"
)

// Start is the fixed clock's reading.
var Start = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// Env is one test's server. Deps.Core and Deps.Actor make changes the way
// the pages do, for arranging a test's starting state.
type Env struct {
	DB     dbtest.DB
	Deps   handlers.Deps
	Server *httptest.Server
	t      testing.TB

	shutdown     chan struct{}
	shutdownOnce sync.Once
}

// Shutdown closes Deps.Shutdown, as the server does when it stops.
func (e *Env) Shutdown() { e.shutdownOnce.Do(func() { close(e.shutdown) }) }

// New boots the router in local mode on a fresh database. opts adjust the
// config before the router is built.
func New(t testing.TB, opts ...func(*config.Config)) *Env {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.FromEnv(func(k string) string { return map[string]string{"VQ_DATA_DIR": dir}[k] })
	if err != nil {
		t.Fatalf("webtest: config: %v", err)
	}
	for _, o := range opts {
		o(&cfg)
	}
	d := dbtest.New(t)
	now := func() time.Time { return Start }
	c := core.New(d.W, now)
	owner, err := c.EnsureOwner(context.Background())
	if err != nil {
		t.Fatalf("webtest: owner: %v", err)
	}
	shutdown := make(chan struct{})
	deps := handlers.Deps{
		Cfg: cfg, DB: d.W, R: d.RQ, Core: c, Actor: owner,
		Logger:     slog.New(slog.DiscardHandler),
		Now:        now,
		Shutdown:   shutdown,
		StreamPoll: 20 * time.Millisecond,
	}
	srv := httptest.NewServer(web.NewRouter(deps))
	e := &Env{DB: d, Deps: deps, Server: srv, t: t, shutdown: shutdown}
	// Streams end before the server closes, or Close would wait on them.
	t.Cleanup(func() { e.Shutdown(); srv.Close() })
	return e
}

// Client is an HTTP client for e's server. Its requests carry
// Host: 127.0.0.1:<port>, as a browser on this machine would send.
type Client struct {
	t    testing.TB
	http *http.Client
	base string
}

// Client returns a new client with its own cookie jar (so a toast set on a
// redirect shows on the next page) that never follows redirects.
func (e *Env) Client() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{t: e.t, base: e.Server.URL, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Follow requests the Location of a redirect response.
func (c *Client) Follow(r *Response) *Response {
	c.t.Helper()
	if r.Location == "" {
		c.t.Fatalf("not a redirect: %d", r.Status)
	}
	return c.Get(r.Location)
}

// Response is what a request returned, body read in full.
type Response struct {
	Status   int
	Location string
	Header   http.Header
	Body     string
}

func (c *Client) do(req *http.Request) *Response {
	c.t.Helper()
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return &Response{Status: res.StatusCode, Location: res.Header.Get("Location"), Header: res.Header, Body: string(b)}
}

func (c *Client) newRequest(method, path string, body io.Reader) *http.Request {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	return req
}

func (c *Client) form(path string, form url.Values) *http.Request {
	req := c.newRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// Get requests path.
func (c *Client) Get(path string) *Response {
	c.t.Helper()
	return c.do(c.newRequest(http.MethodGet, path, nil))
}

// Head requests path with HEAD, as uptime probes and curl -I do.
func (c *Client) Head(path string) *Response {
	c.t.Helper()
	return c.do(c.newRequest(http.MethodHead, path, nil))
}

// GetWithHost requests path with the Host header set to host, as a browser
// does when another site's hostname resolves to this server.
func (c *Client) GetWithHost(host, path string) *Response {
	c.t.Helper()
	req := c.newRequest(http.MethodGet, path, nil)
	req.Host = host
	return c.do(req)
}

// Post submits a form to path.
func (c *Client) Post(path string, form url.Values) *Response {
	c.t.Helper()
	return c.do(c.form(path, form))
}

// PostHX submits a form the way htmx does.
func (c *Client) PostHX(path string, form url.Values) *Response {
	c.t.Helper()
	req := c.form(path, form)
	req.Header.Set("HX-Request", "true")
	return c.do(req)
}

// PostCrossSite submits a form the way a browser would from another site.
func (c *Client) PostCrossSite(path string, form url.Values) *Response {
	c.t.Helper()
	req := c.form(path, form)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	return c.do(req)
}
