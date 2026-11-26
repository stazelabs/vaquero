package help

import (
	"strings"
	"testing"
)

// Help hits match the titles, summaries and headings in memory (VAQ-63):
// every word must start a word somewhere in the article's title, summary
// or headings, any case, any accents.
func TestSearch(t *testing.T) {
	urls := func(q string) []string {
		var out []string
		for _, h := range Search(q, 10) {
			out = append(out, h.URL())
		}
		return out
	}
	for q, want := range map[string]string{
		"backups":         "/help/data-and-backups",
		"BACKUP":          "/help/data-and-backups",
		"compact":         "/help/data-and-backups#compacting-the-database",
		"compacting data": "/help/data-and-backups#compacting-the-database",
		"keyboard":        "/help/keyboard-shortcuts",
		"tokens":          "/help/api-tokens",
		"kéyboard":        "/help/keyboard-shortcuts",
	} {
		got := urls(q)
		if len(got) == 0 || got[0] != want {
			t.Errorf("%q: %v, want %s first", q, got, want)
		}
	}
	for _, q := range []string{"", "   ", "zzzqqq", "backups zzzqqq", `"`, "*"} {
		if got := urls(q); len(got) != 0 {
			t.Errorf("%q: %v, want none", q, got)
		}
	}
	// A title match ranks above a heading-only match.
	got := urls("search")
	if len(got) == 0 || got[0] != "/help/search" {
		t.Errorf("search: %v", got)
	}
	if n := len(Search("the", 2)); n != 2 {
		t.Errorf("limit: %d hits", n)
	}
}

// A hit found by a heading names it, so the page can show where it links.
func TestSearchHitHeading(t *testing.T) {
	hits := Search("compact", 1)
	if len(hits) != 1 || hits[0].Heading != "Compacting the database" || hits[0].Title != "Your data and backups" || !strings.HasPrefix(hits[0].Summary, "Where Vaquero keeps") {
		t.Fatalf("%+v", hits)
	}
	if h := Search("backups", 1); len(h) != 1 || h[0].Heading != "" {
		t.Fatalf("%+v", h)
	}
}

// Article text is folded whole: only the search is cut to maxSearchWords.
func TestFoldWordsKeepsLongText(t *testing.T) {
	if n := len(foldWords(strings.Repeat("word ", 30))); n != 30 {
		t.Fatalf("%d words", n)
	}
	if got := foldWords("Résumé, DEPLOY-ed"); strings.Join(got, " ") != "resume deploy ed" {
		t.Fatalf("%q", got)
	}
}
