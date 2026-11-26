package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/static"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestFooterOnEveryPage(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	for _, path := range []string{"/", "/activity", "/search", "/no-such-page"} {
		contains(t, c.Get(path).Body, "Purple Bongo, Inc.", `href="https://stazelabs.com"`, "Stazelabs project",
			"Vaquero "+version.Version, `href="/about/licenses"`, "Open source notices")
	}
}

func TestLicensesPage(t *testing.T) {
	r := webtest.New(t).Client().Get("/about/licenses")
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "Apache License", "Purple Bongo, Inc.", "htmx", "SortableJS", "Tailwind CSS",
		"github.com/go-chi/chi", "Go standard library", "Permission is hereby granted", "<details")
}

// The header draws the hat inline in theme tokens, so it follows light and
// dark (VAQ-53); static/logo.svg keeps its fixed colours for the favicon.
func TestLogoInHeader(t *testing.T) {
	e := webtest.New(t)
	body := e.Client().Get("/").Body
	contains(t, body, `<svg data-logo`, `fill-accent`, `aria-hidden="true"`)
	if strings.Contains(body, static.URL("logo.svg")) {
		t.Fatal("the header still loads the fixed-colour logo.svg")
	}
	if r := e.Client().Get(static.URL("logo.svg")); r.Status != 200 || !strings.Contains(r.Body, "<svg") || !strings.Contains(r.Body, "#b5532a") {
		t.Fatalf("%d", r.Status)
	}
}
