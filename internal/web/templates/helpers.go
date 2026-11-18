// Package templates holds the templ components. Colours come only from the
// theme tokens in static/css/input.css.
package templates

const (
	// htmxConfig turns off the htmx features the CSP forbids.
	htmxConfig   = `{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false}`
	h1Class      = "text-2xl font-semibold tracking-tight text-ink"
	btnSecondary = "inline-flex items-center rounded-md border border-line bg-paper-alt px-4 py-2 text-sm font-medium text-ink hover:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
)

func pageTitle(t string) string {
	if t == "" {
		return "Vaquero"
	}
	return t + " · Vaquero"
}
