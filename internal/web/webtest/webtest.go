// Package webtest boots the real router on a test database for handler
// tests: a fixed clock, a cookie-free client that never follows redirects.
package webtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/handlers"
)

// Start is the fixed clock's reading.
var Start = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// Instance is the test server's instance id, naming its blob keys.
const Instance = "01jx00000000000000000test00"

// Env is one test's server. Deps.Core and Deps.Actor make changes the way
// the pages do, for arranging a test's starting state; Deps.Blobs is a
// SQLite blob store over the test database, for inspecting stored bytes.
type Env struct {
	DB     dbtest.DB
	Deps   handlers.Deps
	Server *httptest.Server
	t      testing.TB

	shutdown     chan struct{}
	shutdownOnce sync.Once

	// The server's handler is a switch, so Rebuild and Wrap can change
	// what it serves while it serves, without a data race.
	handler handlerSwitch
	mu      sync.Mutex
	wraps   []func(http.Handler) http.Handler
}

// handlerSwitch serves whichever handler was stored last.
type handlerSwitch struct{ h atomic.Pointer[http.Handler] }

func (s *handlerSwitch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.h.Load()).ServeHTTP(w, r)
}

func (e *Env) serve() {
	var h http.Handler = web.NewRouter(e.Deps)
	for _, w := range e.wraps {
		h = w(h)
	}
	e.handler.h.Store(&h)
}

// Rebuild serves a router built from the current Deps, after a test
// changed them. Handlers added by Wrap stay around it.
func (e *Env) Rebuild() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.serve()
}

// Wrap puts mw around the router (and around any earlier wrap). It is
// safe while requests are in flight: later requests go through mw.
func (e *Env) Wrap(mw func(http.Handler) http.Handler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.wraps = append(e.wraps, mw)
	e.serve()
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
	c.BlobStore = "sqlite"
	c.Git = gitread.New()
	owner, err := c.EnsureOwner(context.Background())
	if err != nil {
		t.Fatalf("webtest: owner: %v", err)
	}
	shutdown := make(chan struct{})
	deps := handlers.Deps{
		Cfg: cfg, DB: d.W, R: d.RQ, RDB: d.R, Core: c, Actor: owner, Touch: d.Q, MCPCalls: d.Q,
		Logger:     slog.New(slog.DiscardHandler),
		Now:        now,
		Shutdown:   shutdown,
		StreamPoll: 20 * time.Millisecond,
		BackupDir:  filepath.Join(dir, "backups"),
		Blobs:      blobs.NewSQLite(d.W, d.R, now),
		Instance:   Instance,
		Git:        c.Git,
		Files:      readiness.NewFiles(c.Git),
	}
	e := &Env{DB: d, Deps: deps, t: t, shutdown: shutdown}
	e.serve()
	srv := httptest.NewServer(&e.handler)
	e.Server = srv
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
	// Agent, when set, is sent as Vaquero-Agent on API requests.
	Agent string
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

// Token mints an API token for the owner.
func (e *Env) Token(t testing.TB) string {
	t.Helper()
	raw, _, err := e.Deps.Core.CreateToken(context.Background(), e.Deps.Actor.ID, e.Deps.Actor.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// API sends a JSON API request with token as the bearer ("" for none) and
// body (nil, or encoded as JSON).
func (c *Client) API(method, path, token string, body any) *Response {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req := c.newRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.Agent != "" {
		req.Header.Set("Vaquero-Agent", c.Agent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req)
}

// Upload posts data as the multipart part "file" named filename, the way
// app.js uploads a pasted or dropped file. contentType is the part's
// declared type ("" for application/octet-stream).
func (c *Client) Upload(path, filename, contentType string, data []byte) *Response {
	c.t.Helper()
	return c.UploadAs(path, "", filename, contentType, data)
}

// UploadAs is Upload with token as the bearer ("" for none), for the API.
func (c *Client) UploadAs(path, token, filename, contentType string, data []byte) *Response {
	c.t.Helper()
	return c.Multipart(http.MethodPost, path, token, nil, filename, contentType, data)
}

// Multipart sends a multipart form with method: fields first, then, unless
// filename and data are both empty, data as the part "file" named filename
// (contentType "" for application/octet-stream). token is the bearer (""
// for none).
func (c *Client) Multipart(method, path, token string, fields url.Values, filename, contentType string, data []byte) *Response {
	c.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				c.t.Fatal(err)
			}
		}
	}
	if filename != "" || len(data) > 0 {
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename)+`"`)
		h.Set("Content-Type", contentType)
		pw, err := mw.CreatePart(h)
		if err != nil {
			c.t.Fatal(err)
		}
		if _, err := pw.Write(data); err != nil {
			c.t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		c.t.Fatal(err)
	}
	req := c.newRequest(method, path, &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.Agent != "" {
		req.Header.Set("Vaquero-Agent", c.Agent)
	}
	return c.do(req)
}

// GetHX requests path the way htmx does.
func (c *Client) GetHX(path string) *Response {
	c.t.Helper()
	req := c.newRequest(http.MethodGet, path, nil)
	req.Header.Set("HX-Request", "true")
	return c.do(req)
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
