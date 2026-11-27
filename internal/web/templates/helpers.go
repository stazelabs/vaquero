// Package templates holds the templ components. Colours come only from the
// theme tokens in static/css/input.css.
package templates

import (
	"net/url"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/web/flash"
)

const (
	// htmxConfig turns off the htmx features the CSP forbids.
	htmxConfig   = `{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false}`
	h1Class      = "text-2xl font-semibold tracking-tight text-ink"
	h2Class      = "text-sm font-semibold uppercase tracking-wider text-muted"
	btnBase      = "inline-flex items-center justify-center rounded-md px-4 py-2 text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
	btnPrimary   = btnBase + " bg-accent text-paper hover:bg-accent/90"
	btnSecondary = btnBase + " border border-line bg-paper-alt text-ink hover:border-accent"
	btnDanger    = btnBase + " border border-danger/40 text-danger hover:bg-danger/10"
	btnLink      = "text-sm text-accent hover:underline"
	navLink      = "text-sm font-medium text-ink/70 hover:text-accent"
	navIconLink  = navLink + " inline-flex items-center gap-1.5"
	h2IconClass  = h2Class + " flex items-center gap-1.5"
	panelClass   = "rounded-lg border border-line bg-paper-alt p-5"
	inputClass   = "block w-full rounded-md border border-line bg-paper px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 aria-[invalid=true]:border-danger"
	// fieldClass is a one-line control (text box or select): a fixed height,
	// since browsers size a select's box their own way and it would not line
	// up with a text box beside it. Below sm the text is 1rem (input.css).
	fieldClass = inputClass + " h-9.5 max-sm:h-10.5"
	labelClass = "mb-1 block text-xs font-semibold uppercase tracking-wider text-ink/80"
	proseClass = "prose-vq text-sm leading-relaxed text-ink"
)

func pageTitle(t string) string {
	if t == "" {
		return "Vaquero"
	}
	return t + " · Vaquero"
}

// InputProps configures TextInput and TextArea.
type InputProps struct {
	Name, Label, Type, Value, Placeholder, Autocomplete string
	// ID overrides the element id, "f-" + Name.
	ID       string
	Required bool
	// Rows is TextArea's height in lines (3 when unset).
	Rows int
	// Error is the field's refusal message, shown under it.
	Error string
	// Attrs carries what the field alone needs, such as data-* hooks.
	Attrs templ.Attributes
	// Markdown marks a TextArea whose text renders as Markdown (ticket
	// descriptions and comments): a link to the article goes under it.
	Markdown bool
}

func (p InputProps) id() string {
	if p.ID != "" {
		return p.ID
	}
	return "f-" + p.Name
}

func (p InputProps) inputType() string {
	if p.Type == "" {
		return "text"
	}
	return p.Type
}

func (p InputProps) autocomplete() string {
	if p.Autocomplete == "" {
		return "off"
	}
	return p.Autocomplete
}

func (p InputProps) rows() string {
	if p.Rows <= 0 {
		return "3"
	}
	return strconv.Itoa(p.Rows)
}

func (p InputProps) attrs() templ.Attributes {
	if p.Attrs == nil {
		return templ.Attributes{}
	}
	return p.Attrs
}

// linkTargetAttrs make the link box ask for suggestions as you type.
func linkTargetAttrs(key string) templ.Attributes {
	return templ.Attributes{
		"list":       "link-targets",
		"hx-get":     "/t/" + key + "/link-targets",
		"hx-trigger": "input changed delay:200ms",
		"hx-target":  "#link-targets",
		"hx-swap":    "innerHTML",
		"hx-sync":    "this:replace",
	}
}

// Option is one choice in a Select.
type Option struct{ Value, Label string }

// SelectProps configures Select.
type SelectProps struct {
	Name, Label, Selected, Error string
	Options                      []Option
	Disabled                     bool
}

func toastClass(k flash.Kind) string {
	if k == flash.Error {
		return "bg-danger text-paper"
	}
	return "bg-ink text-paper"
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// shortcuts are the keyboard shortcuts app.js handles, as the dialog lists them.
var shortcuts = [][2]string{
	{"c", "New ticket"},
	{"/", "Search"},
	{"g f", "Focus"},
	{"g b", "Board"},
	{"g l", "List"},
	{"j / k", "Focus the next or previous card"},
	{"enter", "Open the focused card"},
	{"?", "This list"},
	{"esc", "Close"},
}

func adminNavClass(current bool) string {
	if current {
		return "flex items-center gap-2 rounded-md bg-paper-alt px-2 py-1 text-sm font-medium text-ink"
	}
	return "flex items-center gap-2 rounded-md px-2 py-1 text-sm text-ink/70 hover:text-accent"
}

// dayOf is a stored instant's date, or the text as stored if it won't parse.
func dayOf(stamp string) string {
	t, err := timefmt.Parse(stamp)
	if err != nil {
		return stamp
	}
	return timefmt.DayOf(t)
}

// agoOr is how long ago a stored instant was, or none when it is empty.
func agoOr(stamp string, now time.Time, none string) string {
	t, err := timefmt.Parse(stamp)
	if stamp == "" || err != nil {
		return none
	}
	return timefmt.Ago(t, now, nil)
}

func helpNavClass(current bool) string {
	if current {
		return "font-medium text-accent"
	}
	return "text-ink/80 hover:text-accent"
}

// mcpActorLabel is an actor's name for a filter chip, with its kind when
// another actor in the log has the same name (an agent called "Owner").
func mcpActorLabel(a gen.ListMCPCallActorsRow, all []gen.ListMCPCallActorsRow) string {
	for _, o := range all {
		if o.ID != a.ID && o.Name == a.Name {
			return a.Name + " (" + a.Kind + ")"
		}
	}
	return a.Name
}

// mcpAgentListed reports whether the agent filter names an actor in the
// log; an id that matches nothing (an old name-based link) gets its own
// empty state.
func mcpAgentListed(v MCPView) bool {
	for _, a := range v.Agents {
		if a.ID == v.Agent {
			return true
		}
	}
	return false
}

// mcpFilterHref is the MCP activity page filtered by agent and tool.
func mcpFilterHref(agent, tool string) string {
	q := url.Values{}
	if agent != "" {
		q.Set("agent", agent)
	}
	if tool != "" {
		q.Set("tool", tool)
	}
	if len(q) == 0 {
		return "/admin/mcp"
	}
	return "/admin/mcp?" + q.Encode()
}

// utcOf is a stored instant in UTC, for a title.
func utcOf(stamp string) string {
	t, err := timefmt.Parse(stamp)
	if err != nil {
		return stamp
	}
	return timefmt.DisplayUTC(t)
}

// attachAttrs makes an element a paste and drop target for uploads to the
// ticket with this key; app.js reads the address and the size limit.
func attachAttrs(ticketKey string) templ.Attributes {
	if ticketKey == "" {
		return nil
	}
	return templ.Attributes{
		"data-attach-url": "/t/" + ticketKey + "/attachments",
		"data-max-bytes":  strconv.Itoa(core.MaxAttachment),
	}
}

// hasImages reports whether any attachment shows in the gallery.
func hasImages(as []AttachmentRow) bool {
	for _, a := range as {
		if a.Image {
			return true
		}
	}
	return false
}

// hasFiles reports whether any attachment is not an image, so the rows list
// is drawn only when it has rows.
func hasFiles(as []AttachmentRow) bool {
	for _, a := range as {
		if !a.Image {
			return true
		}
	}
	return false
}

// moveQuestion is the confirm dialog's question before a ticket moves to
// another project.
func moveQuestion(v TicketView) string {
	q := "Move " + v.Key + " to the chosen project? It takes a new key there, and " + v.Key + " keeps leading to it."
	if len(v.Subs) > 0 {
		q += " Its sub-tickets move with it."
	}
	return q
}

// removeQuestion is the confirm dialog's question before an attachment is removed.
func removeQuestion(name string) string {
	return "Remove " + name + "? It can't be undone."
}

// revokeQuestion is the confirm dialog's question before a token is revoked.
func revokeQuestion(name string) string {
	if name == "local" {
		return "Revoke local? The CLI gets a new token the next time it starts the server."
	}
	return "Revoke " + name + "? Anything using it stops working."
}

// removeImageNote finishes the Remove image question: what the project
// shows afterwards.
func removeImageNote(kind string) string {
	if kind == core.IconKindImage {
		return " The project shows its monogram instead."
	}
	return " Uploading it again brings it back."
}

// doneCategory names a category, not an icon; the literal passed to
// CategoryIcon would read as an icon name to TestEveryIconNameKnown.
const doneCategory = "done"

// readyIcon is the mark beside "Ready": the done category's.
func readyIcon() string { return CategoryIcon(doneCategory) }
