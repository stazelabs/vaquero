// Package search turns what a person types into a safe full-text query and
// runs it over ticket titles, descriptions and comments.
package search

import (
	"context"
	"database/sql"
	"strings"
	"unicode"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
)

// maxTerms bounds how many words of a long query are searched for.
const maxTerms = 20

// MatchQuery turns typed text into an FTS5 query: each run of letters and
// digits becomes a quoted prefix term ("colum"*), all of them required. No
// FTS5 syntax survives, so no input is a syntax error. "" means there is
// nothing to search for.
func MatchQuery(typed string) string {
	words := strings.FieldsFunc(typed, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > maxTerms {
		words = words[:maxTerms]
	}
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = `"` + w + `"*`
	}
	return strings.Join(terms, " ")
}

// Part is a piece of a snippet; Mark is a matched word.
type Part struct {
	Text string
	Mark bool
}

// Hit is one matching ticket.
type Hit struct {
	Key, Title, Status, Category string
	Snippet                      []Part
}

// Run searches for typed through db (the reader pool). Nothing to search
// for gives no hits.
func Run(ctx context.Context, db *sql.DB, typed string, limit int64) ([]Hit, error) {
	q := MatchQuery(typed)
	if q == "" {
		return nil, nil
	}
	rows, err := appdb.SearchTickets(ctx, db, q, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, len(rows))
	for i, r := range rows {
		hits[i] = Hit{
			Key:      core.TicketKey{Project: r.ProjectKey, Number: r.Number}.String(),
			Title:    r.Title,
			Status:   r.StatusName,
			Category: r.StatusCategory,
			Snippet:  parts(r.Snippet),
		}
	}
	return hits, nil
}

// parts splits a snippet at its \x02 (start) and \x03 (end) markers.
func parts(s string) []Part {
	var out []Part
	mark := false
	for s != "" {
		sep := "\x02"
		if mark {
			sep = "\x03"
		}
		text, rest, found := strings.Cut(s, sep)
		if text != "" {
			out = append(out, Part{Text: text, Mark: mark})
		}
		if !found {
			break
		}
		s, mark = rest, !mark
	}
	return out
}
