package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Plan is what moving docs/BACKLOG.md into Vaquero creates.
type Plan struct {
	ProjectDescription string // the Direction section, verbatim
	Milestones         []Milestone
	Tickets            []Ticket
}

// Milestone is a future phase.
type Milestone struct{ Name, Description string }

// Ticket is one open backlog line.
type Ticket struct {
	Title, Body, Tag, Milestone string
}

// sectionTags names the tag each section's tickets get; any other section
// gets its heading slugged.
var sectionTags = map[string]string{
	"Deferred from the Phase 0 review":              "phase-0-review",
	"Deferred from Phase 1a":                        "phase-1a",
	"Deferred from Phase 1b":                        "phase-1b",
	"Deferred from the footer, key and delete work": "footer-key-delete",
	"Deferred from Phase 2a":                        "phase-2a",
	"Icons and in-app help (requested 2026-11-19)":  "icons-help",
	"Phase 2b review minors":                        "phase-2b-review",
	"Admin section (requested 2026-11-19)":          "admin",
	"Deferred from Phase 3a":                        "phase-3a",
	"Open questions":                                "open-question",
	"Deferred from Phase 3b design (2026-11-20)":    "phase-3b-design",
	"Deferred from Phase 3b":                        "phase-3b",
}

var (
	checkbox   = regexp.MustCompile(`^(\s*)- \[( |x)\] (.*)$`)
	phaseLine  = regexp.MustCompile(`^Phase (\d+)([a-z]?):`)
	phaseRef   = regexp.MustCompile(`\bPhase ([4-7])\b`)
	titleBreak = []string{" — ", "; ", " ("}
)

// Parse reads the backlog's markdown into a plan. Ticked lines are left out
// (their history stays in git); the Phase 3 and 3b lines are the work in
// progress and are left out too; Phases 4 to 7 become milestones.
func Parse(md string) (Plan, error) {
	var p Plan
	var section string
	var direction []string
	var cur *Ticket // the ticket whose continuation lines are still coming
	var curIndent int

	flush := func() { cur = nil }
	for line := range strings.SplitSeq(md, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			section = strings.TrimSpace(h)
			continue
		}
		if strings.HasPrefix(section, "Direction") {
			direction = append(direction, line)
		}
		m := checkbox.FindStringSubmatch(line)
		if m == nil {
			// A deeper, non-blank line continues the open ticket's body.
			if cur != nil && strings.TrimSpace(line) != "" && indent(line) > curIndent {
				cur.Body += "\n" + strings.TrimSpace(line)
				continue
			}
			flush()
			continue
		}
		flush()
		if m[2] == "x" {
			continue
		}
		text := strings.TrimSpace(m[3])
		if section == "Phases" {
			if pm := phaseLine.FindStringSubmatch(text); pm != nil {
				if pm[2] == "" && pm[1] >= "4" && pm[1] <= "7" {
					p.Milestones = append(p.Milestones, Milestone{Name: title(text), Description: text})
				}
				continue
			}
		}
		t := Ticket{Title: title(text), Body: text, Tag: tagFor(section, text)}
		if t.Title == "" {
			return p, fmt.Errorf("section %q: a line has no title: %q", section, line)
		}
		p.Tickets = append(p.Tickets, t)
		cur, curIndent = &p.Tickets[len(p.Tickets)-1], len(m[1])
	}
	// Join a ticket to the first phase milestone its line names.
	for i := range p.Tickets {
		t := &p.Tickets[i]
		t.Body += "\n\nFrom docs/BACKLOG.md, section: " + sectionOf(md, t)
		if r := phaseRef.FindStringSubmatch(firstLine(t.Body)); r != nil {
			for _, ms := range p.Milestones {
				if strings.HasPrefix(ms.Name, "Phase "+r[1]+":") {
					t.Milestone = ms.Name
					break
				}
			}
		}
	}
	p.ProjectDescription = strings.TrimSpace(strings.Join(direction, "\n"))
	return p, nil
}

// sectionOf finds the heading a ticket's first line sits under.
func sectionOf(md string, t *Ticket) string {
	section := ""
	needle := firstLine(t.Body)
	for line := range strings.SplitSeq(md, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			section = strings.TrimSpace(h)
			continue
		}
		if m := checkbox.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[3]) == needle {
			return section
		}
	}
	return section
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// tagFor is the section's tag; under Phases the Linear line is linear-import
// and the rest belong to Phase 3b.
func tagFor(section, text string) string {
	switch {
	case strings.HasPrefix(section, "Direction"):
		return "direction"
	case section == "Phases" && strings.HasPrefix(text, "Linear"):
		return "linear-import"
	case section == "Phases":
		return "phase-3b"
	}
	if t, ok := sectionTags[section]; ok {
		return t
	}
	return slug(section)
}

// title is a line's first clause, without a trailing full stop, cut to 80
// characters on a word with an ellipsis. A parenthetical that comes before
// the clause has said anything ("Destructive buttons (comment delete, ...)
// act on one click") is dropped rather than cut at.
func title(text string) string {
	for {
		cut, at := len(text), ""
		for _, b := range titleBreak {
			if i := strings.Index(text, b); i >= 0 && i < cut {
				cut, at = i, b
			}
		}
		if at == " (" && len([]rune(text[:cut])) < minClause {
			if end := strings.Index(text[cut:], ")"); end > 0 {
				text = text[:cut] + text[cut+end+1:]
				continue
			}
		}
		return shorten(strings.TrimSuffix(strings.TrimSpace(text[:cut]), "."))
	}
}

// minClause is the shortest first clause worth stopping at.
const minClause = 25

func shorten(t string) string {
	r := []rune(t)
	if len(r) <= 80 {
		return t
	}
	t = string(r[:79])
	if r[79] != ' ' {
		if i := strings.LastIndex(t, " "); i > 0 {
			t = t[:i]
		}
	}
	return strings.TrimRight(t, " ,;:") + "…"
}

func indent(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
