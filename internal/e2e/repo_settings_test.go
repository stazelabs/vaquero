//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-136: the Repository section on the settings page, set and saved,
// at phone and desktop widths, light and dark, with no sideways scroll.
func TestE2ERepositorySettings(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "first")
	r.Git("remote", "add", "origin", "https://user:secret@example.com/x.git")
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				newProject(t, env, "REP")
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/p/REP/settings")
				p.typeInto("#f-repo-path", r.Dir)
				p.typeInto("#f-fetch-minutes", "30")
				p.reloaded(func() { p.click("#repository button[type=submit]") },
					`document.body.textContent.includes("https://example.com/x.git")`)
				if eval[bool](p, `document.body.textContent.includes("secret")`) {
					t.Fatal("credentials on the page")
				}
				if dir := os.Getenv("VQ_SHOTS"); dir != "" {
					p.do(chromedp.ScrollIntoView(chromedp.CSS("#repository")))
					buf, err := chromedp.Run(p.ctx, chromedp.Screenshot(chromedp.CSS("#repository")))
					if err != nil {
						t.Fatal(err)
					}
					_ = os.WriteFile(fmt.Sprintf("%s/repo-%d-%s.png", dir, w, scheme), buf, 0o644)
				}
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the page is %d px wide at %d px", sw, w)
				}
			})
		}
	}
}
