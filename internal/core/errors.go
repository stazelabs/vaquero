package core

import (
	"errors"
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
