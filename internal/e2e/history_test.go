//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// A description edit's "Show changes" reads its diff only when opened
// (VAQ-143), once.
func TestHistoryChangesLoadWhenOpened(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	tk := f.ticket("Edited", "", func(in *core.TicketInput) { in.Body = "keep\nold line" })
	body := "keep\nnew line"
	if _, err := env.Deps.Core.PatchTicket(context.Background(), env.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		t.Fatal(err)
	}
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")
	const changes = `details[hx-get*="/changes/"]`
	if eval[bool](p, `document.body.textContent.indexOf("old line") >= 0`) {
		t.Fatal("the page carries the diff before Show changes is opened")
	}
	p.openDetails(changes)
	p.until(`!!document.querySelector(` + js(changes+" pre") + `) && document.querySelector(` + js(changes+" pre") + `).textContent.indexOf("old line") >= 0`)
	if n := p.srv.requests("GET /t/WEB-1/changes/" + itoa(lastSeq(t, env))); n != 1 {
		t.Fatalf("the diff was read %d times, want 1", n)
	}
	// Closing and opening it again reads nothing more.
	p.toggled(changes, func() { p.click(changes + " > summary") })
	p.openDetails(changes)
	if n := p.srv.requests("GET /t/WEB-1/changes/" + itoa(lastSeq(t, env))); n != 1 {
		t.Fatalf("the diff was read %d times after reopening, want 1", n)
	}
}

func lastSeq(t *testing.T, env *webtest.Env) int64 {
	t.Helper()
	n, err := env.Deps.R.MaxEventSeq(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func itoa(n int64) string { return fmt.Sprint(n) }
