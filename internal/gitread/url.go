package gitread

import "strings"

// DisplayURL is a remote URL as Vaquero shows it: a URL with a scheme
// (https://user:token@host/…) loses its userinfo, so a credential written
// into it never reaches a page, an answer or a log. Anything else, such as
// git@host:o/r.git or a local path, keeps its text. Either way it has no
// control characters (C0, DEL, C1), which a terminal would act on.
func DisplayURL(u string) string { return oneLine(displayURL(u)) }

// displayURL is DisplayURL before the controls go.
func displayURL(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return u
	}
	rest := u[i+3:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	at := strings.LastIndexByte(rest[:end], '@')
	if at < 0 {
		return u
	}
	return u[:i+3] + rest[at+1:]
}

// fetchable reports whether a remote's URL, as git resolves it (insteadOf
// applied), is one Fetch may use: https, http, ssh or file, an scp-style
// [user@]host:path, or an absolute local path. Remote helpers (ext::, fd::
// and any other "name::"), git:// (whose core.gitProxy a repo can set) and
// hosts that start with "-" are refused.
func fetchable(u string) bool {
	if u == "" || strings.Contains(u, "::") {
		return false
	}
	for _, r := range u {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if i := strings.Index(u, "://"); i >= 0 {
		switch strings.ToLower(u[:i]) {
		case "https", "http", "ssh", "file":
		default:
			return false
		}
		host := displayURL(u)[i+3:]
		return !strings.HasPrefix(host, "-")
	}
	if strings.HasPrefix(u, "/") {
		return true
	}
	// scp-style: [user@]host:path, with no slash before the colon.
	colon := strings.IndexByte(u, ':')
	if colon <= 0 || strings.Contains(u[:colon], "/") {
		return false
	}
	host := u[:colon]
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	return host != "" && !strings.HasPrefix(host, "-") && !strings.HasPrefix(u, "-")
}

// scrubURLs removes the userinfo of every scheme://user:secret@host URL
// in s, so a credential written into a remote never reaches an error line.
// A URL runs to the next space or quote; the userinfo is everything up to
// the last "@" in it, which may cut a little more than needed (never less).
func scrubURLs(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "://")
		if i < 0 {
			break
		}
		b.WriteString(s[:i+3])
		s = s[i+3:]
		end := strings.IndexAny(s, " \t\r\n'\"`<>")
		if end < 0 {
			end = len(s)
		}
		if at := strings.LastIndexByte(s[:end], '@'); at >= 0 {
			s = s[at+1:]
		}
	}
	b.WriteString(s)
	return b.String()
}
