package handlers_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// formWith is the opening <form> tag whose action is action, or "".
func formWith(body, action string) string {
	re := regexp.MustCompile(`<form[^>]*action="` + regexp.QuoteMeta(action) + `"[^>]*>`)
	return re.FindString(body)
}

// formWithConfirm is the opening tag of the form with that action that
// carries data-confirm, or "".
func formWithConfirm(body, action string) string {
	re := regexp.MustCompile(`<form[^>]*action="` + regexp.QuoteMeta(action) + `"[^>]*>`)
	for _, f := range re.FindAllString(body, -1) {
		if strings.Contains(f, "data-confirm=") {
			return f
		}
	}
	return ""
}

// VAQ-27: destructive forms ask first through the Shell's dialog, and still
// post on their own without JavaScript (a plain method="post" form).
func TestDestructiveFormsAskFirst(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ss := statuses(t, e, p)
	tk, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	cm, err := e.Deps.Core.AddComment(bg, e.Deps.Actor.ID, tk.ID, "Hello.")
	if err != nil {
		t.Fatal(err)
	}
	c := e.Client()

	settings := c.Get("/p/VAQ/settings").Body
	ticket := c.Get("/t/VAQ-1").Body
	for _, tc := range []struct{ page, action, ask string }{
		{settings, "/p/VAQ/statuses/" + ss[0].ID + "/delete", "Delete the status " + ss[0].Name + "?"},
		{settings, "/p/VAQ/archive", "Archive VAQ? It is listed last and takes no new tickets until you restore it."},
		{ticket, "/t/VAQ-1/comments/" + cm.ID + "/delete", "Delete this comment?"},
	} {
		f := formWith(tc.page, tc.action)
		if f == "" {
			t.Errorf("no form for %s", tc.action)
			continue
		}
		if !strings.Contains(f, `method="post"`) {
			t.Errorf("%s: not a plain post form: %s", tc.action, f)
		}
		if !strings.Contains(f, `data-confirm="`+tc.ask+`"`) {
			t.Errorf("%s: want data-confirm %q in %s", tc.action, tc.ask, f)
		}
	}
	// The project delete keeps its typed key and needs no dialog.
	if f := formWith(settings, "/p/VAQ/delete"); strings.Contains(f, "data-confirm") {
		t.Errorf("project delete asks twice: %s", f)
	}
	// The Shell carries the dialog, with a cancel and a confirm button.
	for _, page := range []string{settings, ticket} {
		contains(t, page, `<dialog id="vq-confirm"`, `data-confirm-message`, `data-confirm-cancel`, `data-confirm-ok`)
	}
	// Without JavaScript the form still posts and does the work.
	r := c.Post("/t/VAQ-1/comments/"+cm.ID+"/delete", nil)
	if r.Status != 303 {
		t.Fatalf("delete without the dialog: %d", r.Status)
	}
}

// app.js opens the dialog for [data-confirm] forms and submits the original
// form only on confirm.
func TestAppJSHandlesConfirm(t *testing.T) {
	src, err := os.ReadFile("../static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data-confirm", "vq-confirm", "showModal", "requestSubmit"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("app.js lacks %q", want)
		}
	}
	if strings.Contains(string(src), "confirm(") && regexp.MustCompile(`[^.\w]confirm\(`).Match(src) {
		t.Error("app.js calls the browser's confirm()")
	}
}
