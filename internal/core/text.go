package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const controlMsg = "Remove the hidden control characters."

// hasControl reports a control character other than tab, newline or
// carriage return. A NUL in particular would slip past Go's checks and then
// trip SQLite's (its length() stops at NUL).
func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r'
	})
}

// text trims s and records emptyMsg (when non-empty and s is blank),
// longMsg (when s is over max characters) or controlMsg against field.
func text(f fields, field, s string, max int, emptyMsg, longMsg string) string {
	s = strings.TrimSpace(s)
	switch {
	case hasControl(s):
		f.add(field, controlMsg)
	case s == "" && emptyMsg != "":
		f.add(field, emptyMsg)
	case utf8.RuneCountInString(s) > max:
		f.add(field, longMsg)
	}
	return s
}
