//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// Link suggestions: the ticket page's link box fills its datalist from
// /t/{ticket}/link-targets as it is typed.

const linkBox = `details[data-link-add] input[name="to"]`

// options is the datalist's suggestions, value and label.
func (p *page) options() [][2]string {
	p.t.Helper()
	return eval[[][2]string](p, `Array.prototype.map.call(
		document.querySelectorAll("#link-targets option"),
		function (o) { return [o.value, o.label]; })`)
}

// suggestFor types q into the emptied link box and waits for the
// suggestions answered for exactly q.
func (p *page) suggestFor(q string) [][2]string {
	p.t.Helper()
	eval[bool](p, `(function () {
		if (!window.vqTargetsWatched) {
			window.vqTargetsWatched = true;
			document.body.addEventListener("htmx:afterSettle", function (ev) {
				if (ev.detail.target && ev.detail.target.id === "link-targets") {
					window.vqTargetsFor = new URL(ev.detail.xhr.responseURL).searchParams.get("to");
				}
			});
		}
		window.vqTargetsFor = null;
		return true;
	})()`)
	p.clear(linkBox)
	p.key(q)
	p.until(`window.vqTargetsFor === ` + js(q))
	return p.options()
}

func TestLinkSuggestionsFillAndAdd(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	f.ticket("Source ticket", "")
	f.ticket("Upload retries", "")
	f.ticket("Unrelated work", "")
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")

	// Opening the folded form puts the cursor in the box.
	p.click("details[data-link-add] > summary")
	p.until(`document.activeElement === document.querySelector(` + js(linkBox) + `)`)

	// A number suggests that ticket of this project.
	if got := p.suggestFor("2"); len(got) != 1 || got[0] != [2]string{"WEB-2", "WEB-2 Upload retries"} {
		t.Fatalf("suggestions for 2 = %v, want WEB-2 alone", got)
	}
	// Words of a title suggest by title.
	if got := p.suggestFor("ret"); len(got) != 1 || got[0][0] != "WEB-2" {
		t.Fatalf("suggestions for ret = %v, want WEB-2 alone", got)
	}
	// The ticket itself is never suggested.
	if got := p.suggestFor("ticket"); len(got) != 0 {
		t.Fatalf("suggestions for ticket = %v, want none (only WEB-1 matches)", got)
	}

	// Choosing a suggestion puts its value in the box (the browser's
	// datalist popup isn't drivable headless, so its value is typed), and
	// Add link posts it.
	got := p.suggestFor("upl")
	if len(got) != 1 {
		t.Fatalf("suggestions for upl = %v, want WEB-2", got)
	}
	p.clear(linkBox)
	p.key(got[0][0])
	p.reloaded(func() { p.click(`details[data-link-add] button[type="submit"]`) },
		`!!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Link added.") >= 0`)
	links, err := env.DB.RQ.ListLinksFrom(context.Background(), f.tickets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].OtherID != f.tickets[1].ID {
		t.Fatalf("links from WEB-1 = %+v, want one to WEB-2", links)
	}
}

// clear empties the field sel matches and announces the input, as
// select-all and delete would (chromedp.Clear left the value in place).
func (p *page) clear(sel string) {
	p.t.Helper()
	eval[bool](p, `(function () {
		var el = document.querySelector(`+js(sel)+`);
		el.focus();
		el.value = "";
		el.dispatchEvent(new Event("input", {bubbles: true}));
		return true;
	})()`)
}
