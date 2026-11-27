package main

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

// renamed holds every spelling the command line dropped in run Q (VAQ-161),
// old to new. An old spelling is a usage error naming the new one for a
// release; delete this table, and renamedSpelling's call in run, afterwards.
// A key is one word or two (the first two arguments).
var renamed = map[string]string{
	"project create":   "project new",
	"status add":       "status new",
	"status rename":    "status edit --name NEW",
	"milestone add":    "milestone new",
	"tag rename":       "tag edit --name NEW",
	"ref add":          "ref new",
	"ref remove":       "ref delete",
	"token create":     "token new",
	"token revoke":     "token delete",
	"attach":           "attachment new",
	"attachments move": "attachment move",
	"attachments":      "attachment list",
	"detach":           "attachment delete",
	"comment":          "comment new",
	"link":             "ticket link",
	"unlink":           "ticket unlink",
	"ticket move-to":   "ticket transfer",
	"release":          "unclaim",
	"profile add":      "server new",
	"profile remove":   "server delete",
	"profile list":     "server list",
	"profile use":      "server use",
	"profile":          "server",
}

// renamedSpelling reports, as a usage message, an old spelling in args: a
// command or verb in the table, or the --profile flag.
func renamedSpelling(args []string) (string, bool) {
	msg := func(old, now string) (string, bool) {
		return fmt.Sprintf("vaquero: %q is now %q.", old, now), true
	}
	if len(args) >= 2 {
		if now, ok := renamed[args[0]+" "+args[1]]; ok {
			return msg(args[0]+" "+args[1], now)
		}
	}
	if len(args) >= 1 {
		// comment new is the new spelling; the rest of the table's single
		// words are old whatever follows.
		if now, ok := renamed[args[0]]; ok && !(args[0] == "comment" && len(args) > 1 && args[1] == "new") {
			return msg(args[0], now)
		}
	}
	end := slices.Index(args, "--")
	if end < 0 {
		end = len(args)
	}
	for _, a := range args[:end] {
		if a == "--profile" || a == "-profile" || strings.HasPrefix(a, "--profile=") || strings.HasPrefix(a, "-profile=") {
			return msg("--profile", "--server")
		}
	}
	return "", false
}

// tellRenamed prints the message of an old spelling and says whether args
// was one.
func tellRenamed(stderr io.Writer, args []string) bool {
	m, ok := renamedSpelling(args)
	if ok {
		fmt.Fprintln(stderr, m)
	}
	return ok
}
