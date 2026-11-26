package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const controlMsg = "Remove the hidden control characters."

// HasControl is hasControl, for a client that checks text before sending
// it (vaquero import linear).
func HasControl(s string) bool { return hasControl(s) }

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
	return checked(f, field, strings.TrimSpace(s), max, emptyMsg, longMsg)
}

// checked records the same refusals as text, for an s already trimmed.
func checked(f fields, field, s string, max int, emptyMsg, longMsg string) string {
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

// trimBlock trims trailing whitespace and leading blank lines, keeping the
// indentation of the first non-blank line (a comment may open with an
// indented code block).
func trimBlock(s string) string {
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 || strings.TrimSpace(s[:i]) != "" {
			break
		}
		s = s[i+1:]
	}
	return strings.TrimRightFunc(s, unicode.IsSpace)
}
