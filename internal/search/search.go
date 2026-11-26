// Package search turns what a person types into a safe full-text query and
// runs it over ticket titles, descriptions and comments.
package search

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
)

// maxTerms bounds how many words of a long query are searched for.
const maxTerms = 20

// MatchQuery turns typed text into an FTS5 query: Terms, all of them
// required. No FTS5 syntax survives, so no input is a syntax error. ""
// means there is nothing to search for.
func MatchQuery(typed string) string {
	return strings.Join(Terms(typed), " ")
}

// Terms turns typed text into quoted FTS5 prefix terms ("colum"*): the
// text is put in NFC first, then each run of letters, digits and combining
// marks is a term (so a decomposed "résumé" stays one word), at most
// maxTerms of them. A term holds no quote, star or other syntax.
//
// NFC is applied here, on the query side, and by core when it writes
// titles, descriptions and comments. Text stored decomposed before core
// normalised still matches: the indexes' unicode61 tokenizer with
// remove_diacritics 2 drops combining marks, so both spellings index as the
// same token, and no migration or reindex is needed.
func Terms(typed string) []string {
	typed = norm.NFC.String(typed)
	words := strings.FieldsFunc(typed, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.M, r)
	})
	if len(words) > maxTerms {
		words = words[:maxTerms]
	}
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = `"` + w + `"*`
	}
	return terms
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

// Result is a search's hits, best first, and how many tickets matched in
// all (Total may be more than the hits shown).
type Result struct {
	Hits  []Hit
	Total int
}

// Run searches for typed through db (the reader pool). Nothing to search
// for gives no hits.
func Run(ctx context.Context, db *sql.DB, typed string, limit int64) (Result, error) {
	// A repeated word adds nothing but work.
	var terms []string
	for _, t := range Terms(typed) {
		if !slices.ContainsFunc(terms, func(o string) bool { return strings.EqualFold(o, t) }) {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return Result{}, nil
	}
	rows, err := appdb.SearchTickets(ctx, db, terms, limit)
	if err != nil {
		return Result{}, err
	}
	res := Result{Hits: make([]Hit, len(rows))}
	for i, r := range rows {
		res.Hits[i] = Hit{
			Key:      core.TicketKey{Project: r.ProjectKey, Number: r.Number}.String(),
			Title:    r.Title,
			Status:   r.StatusName,
			Category: r.StatusCategory,
			Snippet:  parts(r.Snippet),
		}
		res.Total = int(r.Total)
	}
	return res, nil
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
