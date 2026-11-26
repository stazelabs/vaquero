//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	cdplog "github.com/chromedp/cdproto/log"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The browser every test opens its tabs in, started once by TestMain.
var (
	browserCtx context.Context
	skipReason string
)

// wait bounds every wait on the page; a test that needs longer is broken.
const wait = 10 * time.Second

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	path := findChrome()
	if path == "" {
		if os.Getenv("VQ_E2E_REQUIRE_CHROME") != "" {
			fmt.Fprintln(os.Stderr, "e2e: no Chrome or Chromium found, and VQ_E2E_REQUIRE_CHROME is set")
			return 1
		}
		skipReason = "no Chrome or Chromium found: install Google Chrome to run the browser tests"
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "vaquero-e2e-chrome-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(path),
		chromedp.UserDataDir(dir), // its own profile, never the person's
		chromedp.WindowSize(1280, 800),
		chromedp.DisableGPU,
	)
	if runtime.GOOS == "linux" {
		// CI runners may refuse Chrome's sandbox; the tabs only open the
		// tests' own loopback servers.
		opts = append(opts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	if err := chromedp.Do(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: start Chrome:", err)
		return 1
	}
	browserCtx = ctx
	code := m.Run()
	// Close the browser and wait for it to exit before the profile goes.
	if err := chromedp.Cancel(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: stop Chrome:", err)
	}
	return code
}

// findChrome is the installed Chrome or Chromium, or "".
func findChrome() string {
	var candidates []string
	if runtime.GOOS == "darwin" {
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	}
	candidates = append(candidates, "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome")
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// page is one test's tab, in a browser context of its own (no cookies
// shared with another test), on env's server.
type page struct {
	t   *testing.T
	ctx context.Context
	env *webtest.Env

	mu       sync.Mutex
	problems []string // uncaught exceptions and CSP refusals
}

// open starts a tab at width × height CSS pixels, light scheme.
func open(t *testing.T, env *webtest.Env, width, height int64) *page {
	t.Helper()
	if browserCtx == nil {
		t.Skip(skipReason)
	}
	tctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	ctx, cancel := chromedp.NewContext(tctx, chromedp.WithNewBrowserContext())
	p := &page{t: t, ctx: ctx, env: env}
	t.Cleanup(func() {
		cancel()
		cancelTimeout()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, s := range p.problems {
			t.Errorf("page: %s", s)
		}
	})
	if err := chromedp.Listen(ctx, func(ev any) {
		var msg string
		switch e := ev.(type) {
		case *cdpruntime.EventExceptionThrown:
			msg = (&chromedp.ExceptionError{ExceptionDetails: e.ExceptionDetails}).Error()
		case *cdplog.EventEntryAdded:
			if e.Entry.Source == cdplog.EntrySourceSecurity {
				msg = "security: " + e.Entry.Text
			}
		}
		if msg != "" {
			p.mu.Lock()
			p.problems = append(p.problems, msg)
			p.mu.Unlock()
		}
	}); err != nil {
		t.Fatalf("listen: %v", err)
	}
	if _, err := chromedp.Call(ctx, cdplog.Enable, cdp.Empty{}); err != nil {
		t.Fatalf("log enable: %v", err)
	}
	p.viewport(width, height)
	p.scheme("light")
	return p
}

// viewport sets the page's true width and height in CSS pixels.
func (p *page) viewport(width, height int64) {
	p.t.Helper()
	if _, err := chromedp.Call(p.ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width: width, Height: height, DeviceScaleFactor: 1,
	}); err != nil {
		p.t.Fatalf("viewport %d×%d: %v", width, height, err)
	}
}

// scheme sets prefers-color-scheme to "light" or "dark".
func (p *page) scheme(s string) {
	p.t.Helper()
	if _, err := chromedp.Call(p.ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
		Features: []*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: s}},
	}); err != nil {
		p.t.Fatalf("scheme %s: %v", s, err)
	}
}

// do runs the actions, failing the test on an error.
func (p *page) do(actions ...chromedp.Action[chromedp.Void]) {
	p.t.Helper()
	if err := chromedp.Do(p.ctx, actions...); err != nil {
		p.t.Fatal(err)
	}
}

// goTo loads path and waits for app.js to have run.
func (p *page) goTo(path string) {
	p.t.Helper()
	p.do(chromedp.Navigate(p.env.Server.URL + path))
	p.until(`document.readyState === "complete" && typeof window.vqToast === "function"`)
}

// eval runs js and decodes its value.
func eval[T any](p *page, js string) T {
	p.t.Helper()
	v, err := chromedp.Run(p.ctx, chromedp.Evaluate[T](js))
	if err != nil {
		p.t.Fatalf("eval %s: %v", js, err)
	}
	return v
}

// until waits for the JavaScript condition to hold.
func (p *page) until(cond string) {
	p.t.Helper()
	if _, err := chromedp.Run(p.ctx, chromedp.Poll[bool](cond, chromedp.WithPollingTimeout(wait))); err != nil {
		p.t.Fatalf("waiting for %s: %v", strings.Join(strings.Fields(cond), " "), err)
	}
}

// reloaded waits for a navigation the page started itself (a redirect, an
// HX-Refresh) to land on a page where cond holds. The old page is marked
// first, so a stale page that already met cond doesn't count.
func (p *page) reloaded(trigger func(), cond string) {
	p.t.Helper()
	eval[bool](p, `window.vqOldPage = true`)
	trigger()
	deadline := time.Now().Add(wait)
	full := `document.readyState === "complete" && !window.vqOldPage && typeof window.vqToast === "function" && (` + cond + `)`
	for {
		// The old page's context is torn down mid-poll; a navigation error
		// means try again on the new one.
		ok, err := chromedp.Run(p.ctx, chromedp.Poll[bool](full, chromedp.WithPollingTimeout(time.Until(deadline))))
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, chromedp.ErrPollingTimeout) {
			st, _ := chromedp.Run(p.ctx, chromedp.Evaluate[string](`JSON.stringify({u: location.href, old: !!window.vqOldPage, rs: document.readyState, toast: (document.getElementById("vq-toast")||{}).textContent, dlg: (document.getElementById("vq-confirm")||{}).open, posts: window.vqPosts, active: document.activeElement && document.activeElement.outerHTML.slice(0, 80)})`))
			p.t.Fatalf("waiting for a new page where %s: %v (page: %s)", cond, err, st)
		}
	}
}

// path is the page's path and query.
func (p *page) path() string {
	p.t.Helper()
	return eval[string](p, `location.pathname + location.search`)
}

// key presses one key (a character, or a kb constant like kb.Escape) the
// way Chrome sends a real key press: one keyDown carrying the key's text,
// then keyUp, so a keydown handler's preventDefault keeps the text out of a
// field. (chromedp.KeyEvent sends the text as a separate char event, which
// preventDefault can't stop.)
func (p *page) key(k string) {
	p.t.Helper()
	for _, r := range k {
		v, ok := kb.Keys[r]
		if !ok {
			p.t.Fatalf("no key for %q", r)
		}
		down := input.DispatchKeyEventParams{
			Type: input.DispatchKeyEventTypeRawKeyDown, Key: v.Key, Code: v.Code,
			WindowsVirtualKeyCode: v.Windows,
		}
		if v.Shift {
			down.Modifiers = int64(kb.ModifierShift)
		}
		if v.Print || r == '\r' {
			down.Type, down.Text, down.UnmodifiedText = input.DispatchKeyEventTypeKeyDown, v.Text, v.Unmodified
		}
		up := down
		up.Type, up.Text, up.UnmodifiedText = input.DispatchKeyEventTypeKeyUp, "", ""
		for _, ev := range []input.DispatchKeyEventParams{down, up} {
			if _, err := chromedp.Call(p.ctx, input.DispatchKeyEvent, ev); err != nil {
				p.t.Fatalf("key %q: %v", r, err)
			}
		}
	}
}

// click clicks the element sel matches, as a mouse would.
func (p *page) click(sel string) {
	p.t.Helper()
	p.do(chromedp.Click(chromedp.CSS(sel), chromedp.NodeVisible))
}

// typeInto focuses the field sel matches and types text, key by key.
func (p *page) typeInto(sel, text string) {
	p.t.Helper()
	p.do(focus(sel))
	p.key(text)
}

// js quotes s as a JavaScript string.
func js(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// escape and enter are the keys' names for key.
var (
	escape = kb.Escape
	enter  = kb.Enter
)

// fixture is a project arranged through core, the way the pages make
// changes.
type fixture struct {
	t       *testing.T
	env     *webtest.Env
	project gen.Project
	status  map[string]gen.Status // by name
	tickets []gen.Ticket          // in the order made
}

func newProject(t *testing.T, env *webtest.Env, key string) *fixture {
	t.Helper()
	ctx := context.Background()
	p, err := env.Deps.Core.CreateProject(ctx, env.Deps.Actor.ID, core.ProjectInput{Key: key, Name: key + " project"})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, env: env, project: p}
	f.reloadStatuses()
	return f
}

func (f *fixture) reloadStatuses() {
	f.t.Helper()
	ss, err := f.env.DB.RQ.ListStatuses(context.Background(), f.project.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	f.status = map[string]gen.Status{}
	for _, s := range ss {
		f.status[s.Name] = s
	}
}

// ticket creates a ticket in the named status ("" for Todo).
func (f *fixture) ticket(title, status string, mod ...func(*core.TicketInput)) gen.Ticket {
	f.t.Helper()
	in := core.TicketInput{Title: title}
	if status != "" {
		s, ok := f.status[status]
		if !ok {
			f.t.Fatalf("no status %q", status)
		}
		in.StatusID = s.ID
	}
	for _, m := range mod {
		m(&in)
	}
	tk, err := f.env.Deps.Core.CreateTicket(context.Background(), f.env.Deps.Actor.ID, f.project.ID, in)
	if err != nil {
		f.t.Fatal(err)
	}
	f.tickets = append(f.tickets, tk)
	return tk
}

// focus focuses the element sel matches.
func focus(sel string) chromedp.Action[chromedp.Void] {
	return chromedp.Focus(chromedp.CSS(sel), chromedp.NodeVisible)
}

// toggled runs action and waits for the details element sel matches to
// fire its toggle event. The event comes a task after the change (and
// app.js answers it: one rail at a time below md), so the next step must
// wait for it, as a person's next tap naturally does.
func (p *page) toggled(sel string, action func()) {
	p.t.Helper()
	el := `document.querySelector(` + js(sel) + `)`
	n := eval[int](p, `(function () {
		if (!window.vqTogglesWatched) {
			window.vqTogglesWatched = true;
			// Capture, after app.js's own capture listener.
			document.addEventListener("toggle", function (ev) {
				ev.target.vqToggles = (ev.target.vqToggles || 0) + 1;
			}, true);
		}
		return `+el+`.vqToggles || 0;
	})()`)
	action()
	p.until(fmt.Sprintf(`(%s.vqToggles || 0) > %d`, el, n))
}

// openDetails opens the details element sel matches by its summary.
func (p *page) openDetails(sel string) {
	p.t.Helper()
	p.toggled(sel, func() { p.click(sel + " > summary") })
	p.until(`document.querySelector(` + js(sel) + `).open`)
}
