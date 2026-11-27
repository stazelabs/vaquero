//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-162: the empty instance and the empty project show the next step
// and the line that connects an agent, readable at phone and desktop
// widths in both colour schemes, with nothing scrolling sideways.
func TestE2EFirstRunEmptyStates(t *testing.T) {
	const line = "claude mcp add vaquero -- vaquero mcp --agent NAME"
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				check := func(path, name string) {
					p.goTo(path)
					if got := eval[string](p, `(document.querySelector('[data-connect-agent] code')||{}).textContent || ""`); got != line {
						t.Fatalf("%s: the connect line is %q", path, got)
					}
					if !eval[bool](p, visible(`[data-connect-agent] code`)) {
						t.Fatalf("%s: the connect line isn't visible", path)
					}
					r := p.rect(`[data-connect-agent] code`)
					if r.Left < 0 || r.Right > float64(w) {
						t.Fatalf("%s: the line spans %v to %v at %d px", path, r.Left, r.Right, w)
					}
					if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
						t.Fatalf("%s is %d px wide at %d px", path, sw, w)
					}
					shot(t, p, "body", fmt.Sprintf("%s-%d-%s", name, w, scheme))
				}
				check("/", "empty-home")
				newProject(t, env, "EMP")
				check("/p/EMP", "empty-project")
			})
		}
	}
}
