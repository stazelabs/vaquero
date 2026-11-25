package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/static"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestHealthz(t *testing.T) {
	r := webtest.New(t).Client().Get("/healthz")
	if r.Status != 200 || r.Body != "ok" {
		t.Fatalf("%d %q", r.Status, r.Body)
	}
}

func TestHeadAnswersLikeGet(t *testing.T) {
	e := webtest.New(t)
	for _, p := range []string{"/healthz", "/"} {
		if r := e.Client().Head(p); r.Status != 200 {
			t.Fatalf("HEAD %s: %d (Location %q)", p, r.Status, r.Location)
		}
	}
}

func TestHomeIsThemed(t *testing.T) {
	r := webtest.New(t).Client().Get("/")
	if r.Status != 200 || !strings.Contains(r.Body, "Vaquero") || !strings.Contains(r.Body, "herds the chaos") {
		t.Fatalf("%d", r.Status)
	}
}

func TestSecurityHeaders(t *testing.T) {
	r := webtest.New(t).Client().Get("/")
	csp := r.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %q: %s", want, csp)
		}
	}
}

func TestStaticAssets(t *testing.T) {
	e := webtest.New(t)
	for _, p := range []string{"css/tailwind.css", "vendor/htmx.min.js", "js/app.js", "favicon.svg"} {
		if r := e.Client().Get(static.URL(p)); r.Status != 200 {
			t.Fatalf("%s: %d", p, r.Status)
		}
	}
}

func TestNotFoundIsThemed(t *testing.T) {
	r := webtest.New(t).Client().Get("/no-such-page")
	if r.Status != 404 || !strings.Contains(r.Body, "Vaquero") {
		t.Fatalf("%d", r.Status)
	}
}

// Review Focus 2
func TestForeignHostRefused(t *testing.T) {
	e := webtest.New(t)
	for _, p := range []string{"/", "/healthz", static.URL("js/app.js")} {
		if r := e.Client().GetWithHost("evil.example", p); r.Status != 403 {
			t.Fatalf("%s: %d", p, r.Status)
		}
	}
	for _, h := range []string{"localhost:7878", "127.0.0.1:7878", "[::1]:7878"} {
		if r := e.Client().GetWithHost(h, "/healthz"); r.Status != 200 {
			t.Fatalf("%s: %d", h, r.Status)
		}
	}
}

func TestCrossSitePostRefused(t *testing.T) {
	if r := webtest.New(t).Client().PostCrossSite("/", nil); r.Status != 403 {
		t.Fatalf("%d", r.Status)
	}
}

func TestNoRawColoursInTemplates(t *testing.T) {
	re := regexp.MustCompile(`#[0-9a-fA-F]{6}\b|oklch\(`)
	files, _ := filepath.Glob("templates/*.templ")
	if len(files) == 0 {
		t.Fatal("no templates found")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if m := re.Find(b); m != nil {
			t.Fatalf("%s uses raw colour %s", f, m)
		}
	}
}

// Review Focus 3: every API route is in the API article, as "METHOD /path".
func TestAPIRoutesDocumented(t *testing.T) {
	a, ok := help.Get("api")
	if !ok {
		t.Fatal("no api article")
	}
	r := web.NewRouter(webtest.New(t).Deps).(chi.Routes)
	n := 0
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if !strings.HasPrefix(route, "/api/v1/") || strings.HasSuffix(route, "*") {
			return nil
		}
		n++
		if !strings.Contains(a.Markdown, "`"+method+" "+route+"`") {
			t.Errorf("%s %s isn't in the api article", method, route)
		}
		return nil
	})
	if n < 20 {
		t.Fatalf("walked %d API routes", n)
	}
}

// VAQ-16: the stylesheet's source stays in the repo; only the built file ships.
func TestStaticDoesNotServeCSSSource(t *testing.T) {
	c := webtest.New(t).Client()
	if r := c.Get("/static/css/input.css"); r.Status != 404 {
		t.Fatalf("input.css: %d", r.Status)
	}
	if r := c.Get(static.URL("css/tailwind.css")); r.Status != 200 {
		t.Fatalf("tailwind.css: %d", r.Status)
	}
}
