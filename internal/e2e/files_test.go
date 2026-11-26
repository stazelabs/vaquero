//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-136: the ticket page with references and commits, and the file view
// (Markdown and plain text), at phone and desktop widths, light and dark.
// Long paths and subjects wrap; nothing scrolls the page sideways.
func TestE2EFileViewAndCommits(t *testing.T) {
	long := "docs/a-very-long-directory-name-that-goes-on/and-on-and-on-without-any-break/specification-of-the-board.md"
	r := gittest.Repo(t)
	r.WriteCommit(long, "# The spec\n\nSome words, then a long one: "+strings.Repeat("x", 120)+"\n\n<script>alert(1)</script>\n", "Write the spec (REP-1)")
	r.WriteCommit("main.go", "package main\n\n// "+strings.Repeat("long-line-", 30)+"\n", "A_subject_with_no_spaces_at_all_that_has_to_wrap_somewhere_on_a_phone (REP-1)")
	r.Tag("v0.1.0")
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				f := newProject(t, env, "REP")
				ctx := context.Background()
				dir, main := r.Dir, "main"
				if _, err := env.Deps.Core.UpdateProject(ctx, env.Deps.Actor.ID, f.project.ID, core.ProjectPatch{RepoPath: &dir, MainBranch: &main}); err != nil {
					t.Fatal(err)
				}
				tk, err := env.Deps.Core.CreateTicket(ctx, env.Deps.Actor.ID, f.project.ID, core.TicketInput{Title: "Board spec"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := env.Deps.Core.AddRef(ctx, env.Deps.Actor.ID, tk.ID, core.RefInput{Kind: "spec", Target: long}); err != nil {
					t.Fatal(err)
				}
				p := open(t, env, w, 900)
				p.scheme(scheme)
				wide := func(what string) {
					t.Helper()
					if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
						t.Errorf("%s is %d px wide at %d px", what, sw, w)
					}
				}
				shot := func(name, sel string) {
					if dir := os.Getenv("VQ_SHOTS"); dir != "" {
						buf, err := chromedp.Run(p.ctx, chromedp.Screenshot(chromedp.CSS(sel)))
						if err != nil {
							t.Fatal(err)
						}
						_ = os.WriteFile(fmt.Sprintf("%s/%s-%d-%s.png", dir, name, w, scheme), buf, 0o644)
					}
				}

				p.goTo("/t/REP-1")
				p.until(`document.querySelector("#commits [data-diffstat]") !== null`)
				if !eval[bool](p, `document.querySelector("#commits").textContent.includes("Released in v0.1.0")`) {
					t.Error("no release in the commits")
				}
				wide("the ticket page")
				shot("commits", "#commits")

				p.reloaded(func() { p.click(`#references a[href^="/p/REP/files/"]`) }, `document.querySelector("[data-file-body]") !== null`)
				if !eval[bool](p, `document.querySelector("[data-file-body] h1") !== null && document.querySelector("[data-file-body] script") === null`) {
					t.Error("the Markdown isn't rendered safely")
				}
				wide("the Markdown file view")
				shot("file-md", "main")

				p.goTo("/p/REP/files/main.go")
				if !eval[bool](p, `document.querySelector("pre[data-file-body]") !== null`) {
					t.Error("no <pre> for a Go file")
				}
				wide("the text file view")
			})
		}
	}
}
