package handlers_test

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

var webpBytes = append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{1}, 32)...)

// iconWorld is a project VAQ with a PNG icon set through core.
func iconWorld(t *testing.T) (*webtest.Env, *webtest.Client) {
	t.Helper()
	e := webtest.New(t)
	p := project(t, e)
	if _, err := e.Deps.Core.SetProjectIcon(bg, e.Deps.Actor.ID, p.ID, "image", "", &core.IconImage{Data: pngBytes}); err != nil {
		t.Fatal(err)
	}
	return e, e.Client()
}

func TestProjectIconServedWithETag(t *testing.T) {
	e, c := iconWorld(t)
	r := c.Get("/p/VAQ/icon")
	if r.Status != 200 || r.Body != string(pngBytes) || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
	etag := r.Header.Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) || r.Header.Get("Cache-Control") != "private, no-cache" ||
		r.Header.Get("X-Content-Type-Options") != "nosniff" || r.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("headers %v", r.Header)
	}
	req, _ := http.NewRequest(http.MethodGet, e.Server.URL+"/p/VAQ/icon", nil)
	req.Header.Set("If-None-Match", etag)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotModified || res.Header.Get("ETag") != etag || res.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("revalidate: %d %v", res.StatusCode, res.Header)
	}
	if r := c.Get("/p/vaq/icon"); r.Status != http.StatusMovedPermanently || r.Location != "/p/VAQ/icon" {
		t.Fatalf("lower case: %d %q", r.Status, r.Location)
	}
	if r := c.Get("/p/NOPE/icon"); r.Status == 200 {
		t.Fatal("served an icon for no project")
	}
}

// Stored data that isn't base64 is a 404, like a missing image, not a 500.
func TestProjectIconUndecodableIs404(t *testing.T) {
	e, c := iconWorld(t)
	if _, err := e.DB.W.ExecContext(bg, `UPDATE project_icons SET data = '%%% not base64'`); err != nil {
		t.Fatal(err)
	}
	if r := c.Get("/p/VAQ/icon"); r.Status != http.StatusNotFound {
		t.Fatalf("%d", r.Status)
	}
}

func TestProjectIconNoneIs404(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	if r := e.Client().Get("/p/VAQ/icon"); r.Status != http.StatusNotFound {
		t.Fatalf("%d", r.Status)
	}
}

func TestAPIProjectIconPatchAndUpload(t *testing.T) {
	e, c, tok := apiWorld(t)
	var p api.Project
	decode(t, c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body, &p)
	if p.Icon == nil || *p.Icon != (api.ProjectIcon{Kind: "monogram"}) {
		t.Fatalf("%+v", p.Icon)
	}
	r := c.API("PATCH", "/api/v1/projects/vaq", tok, map[string]any{"icon": map[string]string{"kind": "glyph", "glyph": "rocket"}})
	decode(t, r.Body, &p)
	if r.Status != 200 || *p.Icon != (api.ProjectIcon{Kind: "glyph", Glyph: "rocket"}) || p.Key != "VAQ" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"icon": map[string]string{"kind": "glyph", "glyph": "unicorn"}})
	if r.Status != 422 || !strings.Contains(r.Body, "Choose a glyph from the list.") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"icon": map[string]string{"kind": "glyph", "colour": "red"}})
	if r.Status != 400 && r.Status != 422 {
		t.Fatalf("unknown field: %d %s", r.Status, r.Body)
	}

	r = c.Multipart("PUT", "/api/v1/projects/VAQ/icon", tok, nil, "logo.webp", "image/png", webpBytes)
	p = api.Project{}
	decode(t, r.Body, &p)
	if r.Status != 200 || *p.Icon != (api.ProjectIcon{Kind: "image", URL: "/p/VAQ/icon"}) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got := c.Get("/p/VAQ/icon"); got.Header.Get("Content-Type") != "image/webp" {
		t.Fatalf("served %q", got.Header.Get("Content-Type"))
	}
	// Every project answer carries the icon, the list included.
	var ps []api.Project
	decode(t, c.API("GET", "/api/v1/projects", tok, nil).Body, &ps)
	if len(ps) != 1 || ps[0].Icon == nil || ps[0].Icon.Kind != "image" {
		t.Fatalf("%+v", ps)
	}

	for _, tc := range []struct {
		name string
		data []byte
		code int
		want string
	}{
		{"logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 422, "Use a PNG, JPEG or WebP image."},
		{"fake.png", []byte("plain words"), 422, "Use a PNG, JPEG or WebP image."},
		{"big.png", append(append([]byte{}, pngBytes...), make([]byte, 300<<10)...), 413, "Use an image of 256 KB or less."},
	} {
		r := c.Multipart("PUT", "/api/v1/projects/VAQ/icon", tok, nil, tc.name, "image/png", tc.data)
		if r.Status != tc.code || !strings.Contains(r.Body, tc.want) {
			t.Errorf("%s: %d %s", tc.name, r.Status, r.Body)
		}
	}
	if r := c.Multipart("PUT", "/api/v1/projects/NOPE/icon", tok, nil, "a.png", "", pngBytes); r.Status != 404 {
		t.Fatalf("%d", r.Status)
	}
	if r := c.Multipart("PUT", "/api/v1/projects/VAQ/icon", "", nil, "a.png", "", pngBytes); r.Status != 401 {
		t.Fatalf("no token: %d", r.Status)
	}
	_ = e
}

func TestSettingsIconSection(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	page := c.Get("/p/VAQ/settings").Body
	for _, want := range []string{`action="/p/VAQ/icon"`, `enctype="multipart/form-data"`, `value="monogram"`, `value="glyph:rocket"`, `name="file"`, "Save icon"} {
		if !strings.Contains(page, want) {
			t.Errorf("settings lack %s", want)
		}
	}

	r := c.Multipart("POST", "/p/VAQ/icon", "", url.Values{"icon": {"glyph:rocket"}}, "", "", nil)
	if r.Status != http.StatusSeeOther || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q %s", r.Status, r.Location, r.Body)
	}
	if page := c.Follow(r).Body; !strings.Contains(page, "Icon saved.") || !strings.Contains(page, `data-icon="glyph-rocket"`) {
		t.Fatal("glyph not shown after save")
	}
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if p.IconKind != "glyph" || p.IconGlyph != "rocket" {
		t.Fatalf("%+v", p)
	}
	// A chosen file wins over the radio, whatever its name claims.
	r = c.Multipart("POST", "/p/VAQ/icon", "", url.Values{"icon": {"monogram"}}, "shot.gif", "image/gif", pngBytes)
	if r.Status != http.StatusSeeOther {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if page := c.Follow(r).Body; !strings.Contains(page, `src="/p/VAQ/icon"`) {
		t.Fatal("image not shown after upload")
	}
	// Refusals return to settings with the message.
	r = c.Multipart("POST", "/p/VAQ/icon", "", nil, "words.png", "image/png", []byte("hello there"))
	if r.Status != http.StatusSeeOther || !strings.Contains(c.Follow(r).Body, "Use a PNG, JPEG or WebP image.") {
		t.Fatalf("%d", r.Status)
	}
	r = c.Multipart("POST", "/p/VAQ/icon", "", nil, "big.png", "image/png", append(append([]byte{}, pngBytes...), make([]byte, 270<<10)...))
	if r.Status != http.StatusSeeOther || !strings.Contains(c.Follow(r).Body, "Use an image of 256 KB or less.") {
		t.Fatalf("%d", r.Status)
	}
	// Over the route's body limit: the themed page, not JSON.
	r = c.Multipart("POST", "/p/VAQ/icon", "", nil, "huge.png", "image/png", make([]byte, 400<<10))
	if r.Status != http.StatusRequestEntityTooLarge || strings.Contains(r.Header.Get("Content-Type"), "json") {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
	// The monogram again.
	r = c.Multipart("POST", "/p/VAQ/icon", "", url.Values{"icon": {"monogram"}}, "", "", nil)
	if r.Status != http.StatusSeeOther {
		t.Fatalf("%d", r.Status)
	}
	if p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ"); p.IconKind != "monogram" {
		t.Fatalf("%+v", p)
	}
}

func TestProjectsPageShowsIcon(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	page := e.Client().Get("/").Body
	if !strings.Contains(page, `data-monogram="VA"`) {
		t.Fatal("no monogram on the projects page")
	}
}
