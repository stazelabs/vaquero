package help

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Hit is an article that matches a search, with the heading it links to
// when a heading is where it matched best.
type Hit struct {
	Slug, Title, Summary string
	// Heading and Anchor name the ## heading the hit links to; both are ""
	// when it links to the top of the article.
	Heading, Anchor string
	score           int
	rank            int
}

// URL is where the hit links: /help/{slug}, or /help/{slug}#{anchor}.
func (h Hit) URL() string {
	if h.Anchor == "" {
		return "/help/" + h.Slug
	}
	return "/help/" + h.Slug + "#" + h.Anchor
}

// maxSearchWords bounds how many words of a long search count, as for
// tickets.
const maxSearchWords = 20

// Where a word was found counts for this much in a hit's score.
const (
	inTitle   = 3
	inHeading = 2
	inSummary = 1
)

// Search finds the articles whose title, summary and ## headings, taken
// together, hold every word of typed, at most limit of them, best first.
// Like ticket search, a word matches the words that start with it, and
// neither case nor accents matter. Only what is in memory is searched, not
// the articles' text. A hit links to the heading that holds the most
// words, unless the title holds them all. Nothing to search for is no
// hits.
func Search(typed string, limit int) []Hit {
	words := foldWords(typed)
	words = slices.Compact(slices.Sorted(slices.Values(words[:min(len(words), maxSearchWords)])))
	if len(words) == 0 || limit <= 0 {
		return nil
	}
	var hits []Hit
	rank := 0
	for _, s := range sections {
		for _, a := range s.Articles {
			rank++
			if h, ok := match(a, words); ok {
				h.rank = rank
				hits = append(hits, h)
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int { return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.rank, b.rank)) })
	return hits[:min(limit, len(hits))]
}

// match scores a against words; ok is false unless every word is found.
func match(a Article, words []string) (Hit, bool) {
	title, summary := foldWords(a.Title), foldWords(a.Summary)
	headings := make([][]string, len(a.Headings))
	for i, h := range a.Headings {
		headings[i] = foldWords(h.Text)
	}
	h := Hit{Slug: a.Slug, Title: a.Title, Summary: a.Summary}
	inTitleCount, best, bestCount := 0, -1, 0
	headingCounts := make([]int, len(headings))
	for _, w := range words {
		score := 0
		if holds(title, w) {
			score = inTitle
			inTitleCount++
		}
		for i, hw := range headings {
			if holds(hw, w) {
				headingCounts[i]++
				score = max(score, inHeading)
			}
		}
		if score == 0 && holds(summary, w) {
			score = inSummary
		}
		if score == 0 {
			return h, false
		}
		h.score += score
	}
	for i, n := range headingCounts {
		if n > bestCount {
			best, bestCount = i, n
		}
	}
	if best >= 0 && inTitleCount < len(words) {
		h.Heading, h.Anchor = a.Headings[best].Text, a.Headings[best].ID
	}
	return h, true
}

// holds reports whether a word of text starts with w.
func holds(text []string, w string) bool {
	return slices.ContainsFunc(text, func(t string) bool { return strings.HasPrefix(t, w) })
}

// foldWords splits s into words, each in lower case with its accents
// dropped (decomposed, then combining marks removed).
func foldWords(s string) []string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.FieldsFunc(b.String(), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
