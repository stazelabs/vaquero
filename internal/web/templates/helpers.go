// Package templates holds the templ components. Colours come only from the
// theme tokens in static/css/input.css.
package templates

import (
	"strconv"

	"github.com/a-h/templ"

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
	panelClass   = "rounded-lg border border-line bg-paper-alt p-5"
	inputClass   = "block w-full rounded-md border border-line bg-paper px-3 py-2 text-sm text-ink placeholder:text-muted focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 aria-[invalid=true]:border-danger"
	labelClass   = "mb-1 block text-xs font-semibold uppercase tracking-wider text-ink/80"
	proseClass   = "prose-vq text-sm leading-relaxed text-ink"
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

// Option is one choice in a Select.
type Option struct{ Value, Label string }

// SelectProps configures Select.
type SelectProps struct {
	Name, Label, Selected, Error string
	Options                      []Option
}

func toastClass(k flash.Kind) string {
	if k == flash.Error {
		return "bg-danger text-paper"
	}
	return "bg-ink text-paper"
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
