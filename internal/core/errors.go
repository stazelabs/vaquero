package core

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrNotFound means the named thing does not exist. Handlers redirect with
// a message rather than showing a 404.
var ErrNotFound = errors.New("not found")

// Invalid is a refused input. Fields maps a form field name to a
// sentence-case message ("Enter a title.").
type Invalid struct{ Fields map[string]string }

// Error joins the messages in field-name order.
func (e *Invalid) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	msgs := make([]string, len(keys))
	for i, k := range keys {
		msgs[i] = e.Fields[k]
	}
	return strings.Join(msgs, " ")
}

// invalid is an *Invalid with one field.
func invalid(field, msg string) *Invalid { return &Invalid{Fields: map[string]string{field: msg}} }

// fields collects messages for one input; err is nil when there are none.
type fields map[string]string

func (f fields) add(field, msg string) {
	if _, ok := f[field]; !ok {
		f[field] = msg
	}
}

func (f fields) err() error {
	if len(f) == 0 {
		return nil
	}
	return &Invalid{Fields: f}
}

// notFound turns sql.ErrNoRows into ErrNotFound and wraps anything else.
func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}
