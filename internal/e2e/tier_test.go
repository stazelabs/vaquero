//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The ticket form's tier select saves, and the ticket page shows the tier.
func TestTierSelectSaves(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "TIE")
	tk := f.ticket("Tiered work", "")
	p := open(t, env, 1280, 900)
	p.goTo("/t/TIE-1/edit")
	eval[bool](p, `(function () { var s = document.querySelector('select[name="tier"]'); s.value = "standard"; return s.value === "standard"; })()`)
	p.reloaded(func() { p.click(`form[action="/t/TIE-1"] button[type="submit"]`) },
		`location.pathname === "/t/TIE-1" && document.body.textContent.includes("Standard")`)
	got, err := env.Deps.R.GetTicket(context.Background(), tk.ID)
	if err != nil || got.Tier != "standard" {
		t.Fatalf("%v tier %q", err, got.Tier)
	}
}
