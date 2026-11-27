//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The confirm dialog: a form[data-confirm] asks in #vq-confirm before it
// posts. Driven on a comment's Delete.

const deleteButton = `form[data-confirm] button[type="submit"]`

func confirmFixture(t *testing.T) (*webtest.Env, *fixture, string) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	tk := f.ticket("Has a comment", "")
	c, err := env.Deps.Core.AddComment(context.Background(), env.Deps.Actor.ID, tk.ID, "Keep me unless confirmed.")
	if err != nil {
		t.Fatal(err)
	}
	return env, f, c.ID
}

func (f *fixture) commentLive(id string) bool {
	f.t.Helper()
	cs, err := f.env.DB.RQ.ListComments(context.Background(), f.tickets[0].ID)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == id {
			return !c.DeletedAt.Valid
		}
	}
	return false
}

// askDelete clicks Delete and waits for the dialog to ask.
func (p *page) askDelete() {
	p.t.Helper()
	eval[int](p, `(window.vqClosesAtAsk = window.vqCloses)`)
	p.click(deleteButton)
	p.until(`document.getElementById("vq-confirm").open || window.vqPosts > 0`)
	if eval[int](p, `window.vqPosts`) > 0 {
		p.t.Fatal("Delete posted without asking")
	}
	msg := eval[string](p, `document.querySelector("[data-confirm-message]").textContent`)
	ok := eval[string](p, `document.querySelector("[data-confirm-ok]").textContent`)
	if msg != "Delete this comment?" || ok != "Delete" {
		p.t.Fatalf("dialog asks %q with %q, want the form's question and its button's label", msg, ok)
	}
}

// recordSubmits notes every submit event and whether app.js stopped it:
// the listener sits on window, after app.js's on document, so a submit it
// lets through is a post under way, seen before the request leaves.
func (p *page) recordSubmits() {
	p.t.Helper()
	eval[bool](p, `(function () {
		window.vqPosts = 0;
		window.addEventListener("submit", function (ev) {
			if (!ev.defaultPrevented) { window.vqPosts++; }
		});
		// The dialog's close event, where app.js puts focus back, comes a
		// task after close(): a test must wait for it, as a person would.
		window.vqCloses = 0;
		document.getElementById("vq-confirm").addEventListener("close", function () { window.vqCloses++; });
		return true;
	})()`)
}

// closedWithoutPosting checks the dialog closed, nothing was posted, and
// focus went back to the Delete button.
func (p *page) closedWithoutPosting(f *fixture, commentID string, events int64, how string) {
	p.t.Helper()
	p.until(`!document.getElementById("vq-confirm").open && window.vqCloses > window.vqClosesAtAsk`)
	if n := eval[int](p, `window.vqPosts`); n != 0 {
		p.t.Fatalf("%s let the form post (%d submits went through)", how, n)
	}
	p.until(`document.activeElement === document.querySelector(` + js(deleteButton) + `)`)
	if n := p.srv.requests("POST /t/WEB-1/comments/" + commentID + "/delete"); n != 0 {
		p.t.Fatalf("%s posted the delete %d times", how, n)
	}
	if !f.commentLive(commentID) || f.events() != events {
		p.t.Fatalf("%s changed something: comment live %v, events %d → %d", how, f.commentLive(commentID), events, f.events())
	}
}

func TestConfirmCancelEscapeAndBackdropDontPost(t *testing.T) {
	env, f, id := confirmFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")
	p.recordSubmits()
	before := f.events()

	// Each way out leaves the comment, and the next Delete asks again:
	// the confirmed flag never sticks.
	p.askDelete()
	if !eval[bool](p, `document.activeElement === document.querySelector("[data-confirm-cancel]")`) {
		t.Fatal("focus didn't move to Cancel when the dialog opened")
	}
	p.click("[data-confirm-cancel]")
	p.closedWithoutPosting(f, id, before, "Cancel")

	p.askDelete()
	p.key(escape)
	p.closedWithoutPosting(f, id, before, "Escape")

	p.askDelete()
	// A click outside the dialog's box lands on its backdrop.
	box := p.rect("#vq-confirm")
	if box.Top < 20 {
		t.Fatalf("no backdrop above the dialog: %+v", box)
	}
	p.do(clickAt(box.cx(), box.Top/2))
	p.closedWithoutPosting(f, id, before, "a backdrop click")

	// A click on the dialog's own padding is not the backdrop.
	p.askDelete()
	p.do(clickAt(box.Left+3, box.Top+3))
	if !eval[bool](p, `document.getElementById("vq-confirm").open`) {
		t.Fatal("a click inside the dialog closed it")
	}
	p.click("[data-confirm-cancel]")
	p.closedWithoutPosting(f, id, before, "Cancel")

	// Focus goes back to the button that submitted, also when a click
	// didn't focus it (as in Safari): here the form is submitted with
	// focus left on the page, so the dialog's own restore would leave it
	// there.
	eval[bool](p, `(function () {
		document.activeElement.blur();
		var b = document.querySelector(`+js(deleteButton)+`);
		b.form.requestSubmit(b);
		return true;
	})()`)
	eval[int](p, `(window.vqClosesAtAsk = window.vqCloses)`)
	p.until(`document.getElementById("vq-confirm").open`)
	p.click("[data-confirm-cancel]")
	p.closedWithoutPosting(f, id, before, "Cancel")
}

func TestConfirmPostsAndRedirectsWithToast(t *testing.T) {
	env, f, id := confirmFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")
	p.recordSubmits()
	before := f.events()
	// Cancel once first, so confirm is the second ask.
	p.askDelete()
	p.click("[data-confirm-cancel]")
	p.closedWithoutPosting(f, id, before, "Cancel")

	p.askDelete()
	p.reloaded(func() { p.click("[data-confirm-ok]") },
		`location.pathname === "/t/WEB-1" && !!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Comment deleted.") >= 0`)
	if n := p.srv.requests("POST /t/WEB-1/comments/" + id + "/delete"); n != 1 {
		t.Fatalf("delete posted %d times, want 1", n)
	}
	if f.commentLive(id) || f.events() != before+1 {
		t.Fatalf("comment live %v, events %d → %d; want deleted with one event", f.commentLive(id), before, f.events())
	}
}

// Cancel and then Delete within one turn of the event loop, before the
// first close event has run (VAQ-141): that late close must not forget
// the new question, so Delete in the reopened dialog posts.
func TestConfirmCancelThenDeleteBackToBack(t *testing.T) {
	env, f, id := confirmFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")
	p.recordSubmits()
	before := f.events()
	p.askDelete()
	eval[bool](p, `(function () {
		document.querySelector("[data-confirm-cancel]").click();
		document.querySelector(`+js(deleteButton)+`).click();
		return true;
	})()`)
	// Both close events and the reopen have run once a task has passed.
	p.until(`window.vqCloses > window.vqClosesAtAsk`)
	if !eval[bool](p, `document.getElementById("vq-confirm").open`) {
		t.Fatal("the second Delete didn't reopen the dialog")
	}
	p.reloaded(func() { p.click("[data-confirm-ok]") },
		`location.pathname === "/t/WEB-1" && !!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Comment deleted.") >= 0`)
	if n := p.srv.requests("POST /t/WEB-1/comments/" + id + "/delete"); n != 1 {
		t.Fatalf("delete posted %d times, want 1", n)
	}
	if f.commentLive(id) || f.events() != before+1 {
		t.Fatalf("comment live %v, events %d → %d; want deleted with one event", f.commentLive(id), before, f.events())
	}
}

// VAQ-212: an attachment's Remove asks first; Cancel leaves it, confirm
// removes it.
func TestConfirmAttachmentRemove(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	f.ticket("Has a file", "")
	if r := env.Client().Upload("/t/WEB-1/attachments", "notes.txt", "", []byte("words\n")); r.Status != 201 {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}
	count := func() int {
		as, err := env.DB.RQ.ListTicketAttachments(context.Background(), f.tickets[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return len(as)
	}
	p := open(t, env, 1280, 800)
	p.goTo("/t/WEB-1")
	p.recordSubmits()
	eval[int](p, `(window.vqClosesAtAsk = window.vqCloses)`)
	p.click(deleteButton)
	p.until(`document.getElementById("vq-confirm").open || window.vqPosts > 0`)
	if eval[int](p, `window.vqPosts`) > 0 {
		t.Fatal("Remove posted without asking")
	}
	if msg := eval[string](p, `document.querySelector("[data-confirm-message]").textContent`); msg != "Remove notes.txt? It can't be undone." {
		t.Fatalf("dialog asks %q", msg)
	}
	p.click("[data-confirm-cancel]")
	p.until(`!document.getElementById("vq-confirm").open && window.vqCloses > window.vqClosesAtAsk`)
	if n := count(); n != 1 || eval[int](p, `window.vqPosts`) != 0 {
		t.Fatalf("Cancel left %d attachments", n)
	}
	p.click(deleteButton)
	p.until(`document.getElementById("vq-confirm").open`)
	p.reloaded(func() { p.click("[data-confirm-ok]") }, `location.pathname === "/t/WEB-1" && !document.querySelector("#attachments form[data-confirm]")`)
	if n := count(); n != 0 {
		t.Fatalf("confirm left %d attachments", n)
	}
}
