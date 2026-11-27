// Package workflow holds Vaquero's workflow prompts and audit pack
// templates: Markdown embedded in the binary, served as MCP prompts
// (prompts/list, prompts/get) and printed by `vaquero workflow`, which
// needs no server. The two give the same text byte for byte, because both
// come from Render.
//
// A prompt is prompts/{name}.md: front matter between --- lines, then the
// body with {{arg}} placeholders.
//
//	---
//	name: audit
//	title: Audit a project
//	description: One sentence.
//	args:
//	  - name: kind
//	    description: One sentence.
//	    required: true
//	    values: qa, hygiene, continuity, docs
//	---
//
// A bad prompt fails at start, so tests catch it. The names are fixed by
// ruling R-R1 (plan, work, triage, audit); List gives the shipped ones in
// that order. A pack template is packs/{kind}.md, printed as written.
//
// Every backticked MCP tool, `vaquero …` command and /api/v1 route in a
// prompt or a template must exist (R-R3); cmd/vaquero's
// TestWorkflowTextsNameOnlyWhatExists holds that, because only that
// package sees the CLI's commands.
package workflow

import (
	"bytes"
	"embed"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/stazelabs/vaquero/internal/units"
)

//go:embed prompts/*.md packs/*.md
var content embed.FS

// Arg is one of a prompt's arguments. Values, when set, are the only
// values allowed.
type Arg struct {
	Name, Description string
	Required          bool
	Values            []string
}

// Prompt is one workflow prompt. Body is Markdown with {{name}}
// placeholders, one per argument.
type Prompt struct {
	Name, Title, Description string
	Args                     []Arg
	Body                     string
}

// ArgError is a missing required argument or a value it doesn't allow.
type ArgError struct {
	Name    string
	Allowed []string
}

func (e *ArgError) Error() string { return e.Say("") }

// Say is the message with the argument's name after prefix, such as "--"
// for a flag: "Give --kind: qa, hygiene, continuity or docs."
func (e *ArgError) Say(prefix string) string {
	if len(e.Allowed) == 0 {
		return "Give " + prefix + e.Name + "."
	}
	return "Give " + prefix + e.Name + ": " + Allowed(e.Allowed) + "."
}

// Allowed lists values in words: "a, b or c".
func Allowed(values []string) string {
	if len(values) <= 1 {
		return strings.Join(values, "")
	}
	return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
}

// MaxValue is how many characters of an argument's value a prompt keeps.
const MaxValue = 80

// none stands for an optional argument that wasn't given.
const none = "(none given)"

// order is every prompt name ruling R-R1 fixes, in its order.
var order = []string{"plan", "work", "triage", "audit"}

// packKinds are the audit pack templates, in order.
var packKinds = []string{"qa", "hygiene", "continuity", "docs"}

var (
	prompts     []Prompt
	packs       = map[string]string{}
	argNameRE   = regexp.MustCompile(`^[a-z][a-z-]*$`)
	placeholder = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
)

func init() {
	if err := load(); err != nil {
		panic("workflow: " + err.Error())
	}
}

func load() error {
	files, err := content.ReadDir("prompts")
	if err != nil {
		return err
	}
	for _, f := range files {
		name := strings.TrimSuffix(f.Name(), ".md")
		if !slices.Contains(order, name) {
			return fmt.Errorf("prompts/%s: not a prompt name (%s)", f.Name(), strings.Join(order, ", "))
		}
		src, err := content.ReadFile(path.Join("prompts", f.Name()))
		if err != nil {
			return err
		}
		p, err := parsePrompt(name, src)
		if err != nil {
			return err
		}
		prompts = append(prompts, p)
	}
	slices.SortFunc(prompts, func(a, b Prompt) int { return slices.Index(order, a.Name) - slices.Index(order, b.Name) })
	files, err = content.ReadDir("packs")
	if err != nil {
		return err
	}
	for _, f := range files {
		kind := strings.TrimSuffix(f.Name(), ".md")
		if !slices.Contains(packKinds, kind) {
			return fmt.Errorf("packs/%s: not a pack kind", f.Name())
		}
		b, err := content.ReadFile(path.Join("packs", f.Name()))
		if err != nil {
			return err
		}
		packs[kind] = string(b)
	}
	for _, k := range packKinds {
		if packs[k] == "" {
			return fmt.Errorf("packs/%s.md: missing or empty", k)
		}
	}
	return nil
}

// parsePrompt reads a prompt's front matter and checks its body's
// placeholders.
func parsePrompt(name string, src []byte) (Prompt, error) {
	p := Prompt{}
	fail := func(format string, a ...any) (Prompt, error) {
		return Prompt{}, fmt.Errorf("%s: %s", name, fmt.Sprintf(format, a...))
	}
	rest, ok := bytes.CutPrefix(src, []byte("---\n"))
	if !ok {
		return fail("no front matter")
	}
	head, body, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return fail("front matter not closed")
	}
	inArgs := false
	for line := range strings.SplitSeq(string(head), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var a *Arg
		text := line
		switch {
		case inArgs && strings.HasPrefix(line, "  - "):
			p.Args = append(p.Args, Arg{})
			a, text = &p.Args[len(p.Args)-1], line[4:]
		case inArgs && strings.HasPrefix(line, "    ") && len(p.Args) > 0:
			a, text = &p.Args[len(p.Args)-1], line[4:]
		case strings.HasPrefix(line, " "):
			return fail("front matter line %q", line)
		}
		k, v, found := strings.Cut(strings.TrimSpace(text), ":")
		if !found {
			return fail("front matter line %q", line)
		}
		k, v = strings.TrimSpace(k), unquote(strings.TrimSpace(v))
		if a != nil {
			switch k {
			case "name":
				a.Name = v
			case "description":
				a.Description = v
			case "required":
				if v != "true" && v != "false" {
					return fail("required is true or false, not %q", v)
				}
				a.Required = v == "true"
			case "values":
				for s := range strings.SplitSeq(v, ",") {
					if s = strings.TrimSpace(s); s == "" {
						return fail("an empty value in %q", v)
					}
					a.Values = append(a.Values, s)
				}
			default:
				return fail("unknown argument field %q", k)
			}
			continue
		}
		inArgs = false
		switch k {
		case "name":
			p.Name = v
		case "title":
			p.Title = v
		case "description":
			p.Description = v
		case "args":
			if v != "" {
				return fail("args is a list")
			}
			inArgs = true
		default:
			return fail("unknown field %q", k)
		}
	}
	if p.Name != name {
		return fail("name %q isn't the file's", p.Name)
	}
	if p.Title == "" || p.Description == "" {
		return fail("needs a title and a description")
	}
	seen := map[string]bool{}
	for _, a := range p.Args {
		if !argNameRE.MatchString(a.Name) || seen[a.Name] {
			return fail("argument name %q", a.Name)
		}
		seen[a.Name] = true
	}
	p.Body = strings.TrimSpace(string(body)) + "\n"
	if p.Body == "\n" {
		return fail("empty body")
	}
	if err := checkPlaceholders(p); err != nil {
		return fail("%v", err)
	}
	return p, nil
}

// checkPlaceholders refuses a body whose {{…}} don't all name arguments.
func checkPlaceholders(p Prompt) error {
	for _, m := range placeholder.FindAllStringSubmatch(p.Body, -1) {
		if !slices.ContainsFunc(p.Args, func(a Arg) bool { return a.Name == m[1] }) {
			return fmt.Errorf("placeholder %s names no argument", m[0])
		}
	}
	if rest := placeholder.ReplaceAllString(p.Body, ""); strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return fmt.Errorf("stray {{ or }} in the body")
	}
	return nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// List is the shipped prompts, in R-R1's order.
func List() []Prompt { return slices.Clone(prompts) }

// Get is the prompt called name.
func Get(name string) (Prompt, bool) {
	i := slices.IndexFunc(prompts, func(p Prompt) bool { return p.Name == name })
	if i < 0 {
		return Prompt{}, false
	}
	return prompts[i], true
}

// Packs is the audit pack templates' kinds, in order.
func Packs() []string { return slices.Clone(packKinds) }

// Pack is the template for kind, as written.
func Pack(kind string) (string, bool) { b, ok := packs[kind]; return b, ok }

// heading is one of a pack's ## sections; an optional one may be absent.
type heading struct {
	name     string
	optional bool
}

// headings are each kind's pack sections, in order: the stazelabs
// docs/audit-packs.md headings (R-R4), the docs kind taking update-docs'.
// The audit prompt finds a section by its exact heading.
var headings = map[string][]heading{
	"qa": {{name: "Standards"}, {name: "Never targets"}, {name: "Boot"}, {name: "Areas"}, {name: "Limits"},
		{name: "Triage"}, {name: "Code map"}, {name: "Cleanup"}, {name: "Known non-findings"}},
	"hygiene": {{name: "Standards"}, {name: "Pre-pass"}, {name: "Dimensions"}, {name: "Search surface"},
		{name: "Known non-findings"}, {name: "Verification notes", optional: true}},
	"continuity": {{name: "Standards"}, {name: "Dimensions"}, {name: "Methods"}, {name: "Search surface"},
		{name: "Known non-findings"}},
	"docs": {{name: "Standards"}, {name: "Ground truth"}, {name: "Surfaces"}, {name: "Frozen"}, {name: "Partitions"},
		{name: "Voice"}, {name: "Extra rules", optional: true}, {name: "Verify"}},
}

// RequiredHeadings is the ## headings a pack of kind must have, in order,
// without the optional ones; nil for a kind that isn't one.
func RequiredHeadings(kind string) []string {
	var out []string
	for _, h := range headings[kind] {
		if !h.optional {
			out = append(out, h.name)
		}
	}
	return out
}

// MissingHeadings is the required headings a pack of kind lacks: each
// must be a whole "## Name" line (trailing spaces aside).
func MissingHeadings(kind, pack string) []string {
	have := packHeadings(pack)
	var out []string
	for _, h := range RequiredHeadings(kind) {
		if !slices.Contains(have, h) {
			out = append(out, h)
		}
	}
	return out
}

// packHeadings is a pack's ## headings, in order.
func packHeadings(pack string) []string {
	var out []string
	for line := range strings.SplitSeq(pack, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimRight(line, " \t\r"), "## "); ok {
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

// Render is p's body with each {{name}} replaced by its argument's value:
// control characters and line breaks turned to spaces, trimmed, cut at
// MaxValue characters, so a value is plain text on one line. A required
// argument that is missing or blank, or a value outside the allowed ones
// (matched in any case, written as listed), is an *ArgError; an optional
// one missing reads "(none given)". Arguments p doesn't take are ignored.
// A {{…}} left over is an error, never a blank in the text.
func Render(p Prompt, args map[string]string) (string, error) {
	values := map[string]string{}
	for _, a := range p.Args {
		v := clean(args[a.Name])
		switch {
		case v == "" && a.Required:
			return "", &ArgError{Name: a.Name, Allowed: slices.Clone(a.Values)}
		case v == "":
			v = none
		case len(a.Values) > 0:
			i := slices.IndexFunc(a.Values, func(s string) bool { return strings.EqualFold(s, v) })
			if i < 0 {
				return "", &ArgError{Name: a.Name, Allowed: slices.Clone(a.Values)}
			}
			v = a.Values[i]
		}
		values[a.Name] = v
	}
	if err := checkPlaceholders(p); err != nil {
		return "", fmt.Errorf("prompt %s: %w", p.Name, err)
	}
	return placeholder.ReplaceAllStringFunc(p.Body, func(m string) string { return values[m[2:len(m)-2]] }), nil
}

// clean makes v one line of plain text, at most MaxValue characters.
func clean(v string) string {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\u0085' {
			return ' '
		}
		return r
	}, v)
	return strings.TrimSpace(units.Clip(strings.TrimSpace(v), MaxValue))
}

// SampleArgs is a set of arguments for p per allowed value of its
// arguments (one set when none has allowed values), for tests that render
// every prompt.
func SampleArgs(p Prompt) []map[string]string {
	base := map[string]string{}
	n := 1
	for _, a := range p.Args {
		switch {
		case len(a.Values) > 0:
			n = max(n, len(a.Values))
		case a.Name == "ticket":
			base[a.Name] = "VAQ-12"
		case a.Name == "project":
			base[a.Name] = "VAQ"
		default:
			base[a.Name] = "example"
		}
	}
	var out []map[string]string
	for i := range n {
		m := map[string]string{}
		maps.Copy(m, base)
		for _, a := range p.Args {
			if len(a.Values) > 0 {
				m[a.Name] = a.Values[i%len(a.Values)]
			}
		}
		out = append(out, m)
	}
	return out
}
