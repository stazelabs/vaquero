package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TicketKey names a ticket by its project key and number: VAQ-12.
type TicketKey struct {
	Project string
	Number  int64
}

var ticketKeyRe = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-0*([1-9][0-9]{0,17})$`)

// ParseTicketKey reads a key typed by a person: surrounding space and
// letter case are ignored and leading zeros are dropped ("vaq-012" is
// VAQ-12). It reports false for anything else.
func ParseTicketKey(s string) (TicketKey, bool) {
	m := ticketKeyRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return TicketKey{}, false
	}
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return TicketKey{}, false
	}
	return TicketKey{Project: m[1], Number: n}, true
}

// String is the canonical form, "VAQ-12".
func (k TicketKey) String() string { return fmt.Sprintf("%s-%d", k.Project, k.Number) }
