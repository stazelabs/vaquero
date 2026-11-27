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
	// htmxConfig turns off the htmx features the CSP forbids, and settles a
	// swap at once: new content is bound (hx-post and the rest) in the same
	// task, so a click just after a live refresh never posts the page.
	htmxConfig   = `{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"defaultSettleDelay":0}`
	h1Class      = "text-lg font-semibold tracking-tight text-ink"
	h2Class      = "text-base font-semibold text-ink"
	btnBase      = "inline-flex h-8 items-center justify-center gap-1.5 rounded-sm px-3 text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 max-sm:h-10.5"
	btnPrimary   = btnBase + " bg-accent text-paper hover:bg-accent/90"
	btnSecondary = btnBase + " border border-line bg-paper text-ink hover:border-accent"
	btnDanger    = btnBase + " border border-danger/40 text-danger hover:bg-danger/10"
	btnLink      = "text-sm text-accent hover:underline"
	navLink      = "text-sm font-medium text-ink/70 hover:text-accent"
	navIconLink  = navLink + " inline-flex items-center gap-1.5"
	h2IconClass  = h2Class + " flex items-center gap-1.5"
	panelClass   = "rounded-sm border border-line bg-paper-alt p-4"
	inputBase    = "block w-full rounded-sm border border-line bg-paper px-2.5 text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 aria-[invalid=true]:border-danger"
	inputClass   = inputBase + " py-1.5"
	// fieldClass is a one-line control (text box or select): a fixed height,
	// since browsers size a select's box their own way and it would not line
	// up with a text box beside it; buttons share it. Below sm the text is
	// 1rem (input.css), so both grow.
	fieldClass = inputBase + " h-8 max-sm:h-10.5"
	labelClass = "mb-1 block text-xs font-semibold uppercase tracking-wider text-ink/80"
	// rowClass is a ticket as one 28 px line (Focus's lanes, the list): a
	// priority mark, the number, the title (rowTitle, cut with an
	// ellipsis), then the trailing meta. Rows are divided by line rules.
	rowClass = "flex h-row min-w-0 items-center gap-2 border-b border-line px-1.5 text-sm hover:bg-paper/60 focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent"
	// rowOnPaper is a row on the page's paper (the list, the inbox, My
	// work), whose hover is paper-alt; rowClass's paper hover is for
	// Focus's lanes, which are paper-alt.
	rowOnPaper = "flex h-row min-w-0 items-center gap-2 border-b border-line px-1.5 text-sm hover:bg-paper-alt focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent"
	rowTitle   = "min-w-0 flex-1 truncate text-ink"
	// rowMeta is a row's trailing part: faces, claims, a day. It never
	// wraps; a long reason is cut too.
	rowMeta    = "ml-auto flex max-w-1/2 shrink-0 items-center gap-1.5 text-xs text-muted"
	proseClass = "prose-vq text-sm leading-relaxed text-ink"
)

// chipClass is a 24 px filter chip, filled with the accent when on.
func chipClass(on bool) string {
	base := "inline-flex h-chip items-center rounded-sm border px-2 text-xs "
	if on {
		return base + "border-accent bg-accent text-paper"
	}
	return base + "border-line text-ink/80 hover:border-accent"
}

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
	{"⌘K / Ctrl+K", "Search, even while typing"},
	{"g i", "Inbox"},
	{"g f", "Focus"},
	{"g b", "Board"},
	{"g l", "List"},
	{"j / k", "Focus the next or previous card"},
	{"enter", "Open the focused card"},
	{"1–9", "In the inbox, choose an option"},
	{"n", "In the inbox, add a note"},
	{"o", "In the inbox, open the ticket"},
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

// The sidebar's recipes (VAQ-171). navRow is a 26 px row; navItemClass
// marks the current item (data-current) with the paper background, a
// 2 px accent bar on its left and weight 600.
const (
	sidebarClass = "flex w-52 shrink-0 flex-col overflow-y-auto overscroll-contain border-r border-line bg-paper-alt px-2 pb-2 " +
		"max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-40 max-md:hidden max-md:shadow-lg max-md:data-open:flex md:sticky md:top-0 md:h-screen md:self-start"
	navRow          = "flex h-nav min-w-0 items-center gap-2 rounded-sm px-2 text-sm text-ink/80 hover:bg-paper/60 hover:text-ink focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent"
	navItemClass    = navRow + " data-current:bg-paper data-current:font-semibold data-current:text-ink data-current:shadow-[inset_2px_0_0_var(--color-accent)]"
	navProjectClass = navRow + " aria-[current=page]:font-semibold aria-[current=page]:text-ink"
	navFootButton   = "inline-flex h-nav w-nav shrink-0 items-center justify-center rounded-sm text-ink/70 hover:bg-paper/60 hover:text-accent"
	// barTab is a view tab in the top bar; the current one is filled.
	barTab = "inline-flex h-chip shrink-0 items-center gap-1.5 rounded-sm px-2 text-sm text-ink/70 hover:text-accent aria-[current=page]:bg-paper-alt aria-[current=page]:font-semibold aria-[current=page]:text-ink"
	// barAction is the top bar's main action (New ticket).
	barAction = "inline-flex h-chip shrink-0 items-center gap-1 rounded-sm bg-accent px-2 text-xs font-medium text-paper hover:bg-accent/90 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
)

// projectViewPath is the address of project key's view.
func projectViewPath(key, view string) string {
	if view == "focus" {
		return "/p/" + key
	}
	return "/p/" + key + "/" + view
}

// pageName is the top bar's name for a page without a bar of its own.
func pageName(title string) string {
	if title == "" {
		return "Vaquero"
	}
	return title
}
