package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// HelpIndex is GET /help: every article by section.
func HelpIndex(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, d, http.StatusOK, templates.HelpIndexPage(help.Sections()))
	}
}

// HelpArticle is GET /help/{slug}, or /help/{slug}.md for the article as
// markdown. Articles come only from the embedded set; anything else is 404.
func HelpArticle(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug, asMarkdown := strings.CutSuffix(chi.URLParam(r, "slug"), ".md")
		a, ok := help.Get(slug)
		if !ok {
			NotFound(d)(w, r)
			return
		}
		if asMarkdown {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte(a.Markdown))
			return
		}
		render(w, r, d, http.StatusOK, templates.HelpArticlePage(a, help.SectionOf(a)))
	}
}
