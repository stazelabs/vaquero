package core

import (
	"strings"
	"unicode/utf8"
)

// text trims s and records emptyMsg (when non-empty and s is blank) or
// longMsg (when s is over max characters) against field.
func text(f fields, field, s string, max int, emptyMsg, longMsg string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "" && emptyMsg != "":
		f.add(field, emptyMsg)
	case utf8.RuneCountInString(s) > max:
		f.add(field, longMsg)
	}
	return s
}
