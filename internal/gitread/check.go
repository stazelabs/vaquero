package gitread

import (
	"strings"
	"unicode/utf8"
)

// MaxPathLen and MaxRevLen bound what reaches git.
const (
	MaxPathLen = 4096
	MaxRevLen  = 255
)

// ValidPath reports whether p is a repo path Vaquero passes to git: relative
// and clean (no leading "/", no empty, "." or ".." part), not starting with
// "-" or ":" (an option, or pathspec magic), no ".git" part, no backslash and
// no control character (C0, DEL or C1), valid UTF-8. Spaces are fine.
func ValidPath(p string) bool {
	if p == "" || len(p) > MaxPathLen || !utf8.ValidString(p) {
		return false
	}
	if p[0] == '-' || p[0] == ':' || p[0] == '/' {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\\' {
			return false
		}
	}
	for part := range strings.SplitSeq(p, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

// ValidRev reports whether s is a revision Vaquero passes to git: a commit
// hash (7 to 64 hex digits) or a ref name such as main, v0.1.6, run-o or
// origin/main. Ref names are letters, digits and . _ / + -, start with a
// letter, digit or underscore, and follow git's ref rules: no "..", no "//",
// no part starting with "." or ending with ".lock", no trailing "/" or ".".
// Anything git reads as revision syntax (~ ^ : @{ and the like) is refused.
func ValidRev(s string) bool {
	if s == "" || len(s) > MaxRevLen {
		return false
	}
	c := s[0]
	if !(isAlnum(c) || c == '_') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(isAlnum(c) || c == '.' || c == '_' || c == '/' || c == '+' || c == '-') {
			return false
		}
	}
	if strings.Contains(s, "..") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") {
		return false
	}
	for part := range strings.SplitSeq(s, "/") {
		if part == "" || part[0] == '.' || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// validRemote is a remote's name: a ref name part, so it can't climb out of
// refs/remotes/{remote}/.
func validRemote(s string) bool {
	return ValidRev(s) && !strings.Contains(s, "/") && len(s) <= 100
}

// validKey is a ticket key as Mentions greps for it: letters and digits, a
// hyphen and a number (VAQ-12).
func validKey(s string) bool {
	i := strings.LastIndexByte(s, '-')
	if i < 1 || i == len(s)-1 || len(s) > 32 {
		return false
	}
	for j := 0; j < len(s); j++ {
		c := s[j]
		switch {
		case j == i:
		case j < i && (isAlnum(c)) && !(j == 0 && c >= '0' && c <= '9'):
		case j > i && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// isHash reports whether s is a full or abbreviated commit hash.
func isHash(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
