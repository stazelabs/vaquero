package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestTicketPageListsAttachments(t *testing.T) {
	_, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "image/png", pngBytes))
	body := c.Get("/t/VAQ-1").Body
	contains(t, body, "Attachments", `href="/attachments/`+u.ID+`/shot.png"`, "shot.png", "1 KB", "Owner",
		`data-attach-url="/t/VAQ-1/attachments"`, "Choose files", `type="file"`, "data-live", `id="attachments"`,
		`data-max-bytes="10485760"`)
	if n := strings.Count(body, `data-attach-url="/t/VAQ-1/attachments"`); n != 2 {
		t.Errorf("want the comment box and the well, got %d", n)
	}
}

func TestDeleteButtonOnlyForUploaderOrOwner(t *testing.T) {
	e, c := attachEnv(t)
	mine := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "owner.png", "image/png", pngBytes))
	agent, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "scout")
	if err != nil {
		t.Fatal(err)
	}
	tk, _, err := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: 1})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := e.Deps.Core.AddAttachment(bg, agent.ID, core.AttachmentInput{
		ID: "01JZZZZZZZZZZZZZZZZZZZZZZ1", TicketID: tk.ID, Name: "agent.png", ContentType: "image/png",
		BlobKey: "attachments/x/01jzzzzzzzzzzzzzzzzzzzzzz1", Size: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	del := func(id string) string { return `/t/VAQ-1/attachments/` + id + `/delete` }

	// The owner may remove both.
	contains(t, c.Get("/t/VAQ-1").Body, del(mine.ID), del(theirs.ID))

	// An agent may remove only its own.
	e.Deps.Actor = agent
	e.Rebuild()
	body := e.Client().Get("/t/VAQ-1").Body
	contains(t, body, del(theirs.ID))
	lacks(t, body, del(mine.ID))
}

func TestNewTicketFormHasNoAttach(t *testing.T) {
	_, c := attachEnv(t)
	body := c.Get("/p/VAQ/new").Body
	contains(t, body, "Attach files after creating the ticket.")
	lacks(t, body, "data-attach-url")
	// The edit form can attach.
	contains(t, c.Get("/t/VAQ-1/edit").Body, `data-attach-url="/t/VAQ-1/attachments"`)
}

func TestTicketPageListsImageOnce(t *testing.T) {
	_, c := attachEnv(t)
	img := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "image/png", pngBytes))
	txt := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "notes.txt", "text/plain", []byte("hello")))
	body := c.Get("/t/VAQ-1").Body
	start := strings.Index(body, `id="attachments"`)
	end := strings.Index(body[start:], "data-attach-well")
	if start < 0 || end < 0 {
		t.Fatal("attachments section not found")
	}
	sec := body[start : start+end]
	if n := strings.Count(sec, `href="/attachments/`+img.ID+`/shot.png"`); n != 1 {
		t.Errorf("image linked %d times, want 1 (gallery only)", n)
	}
	if n := strings.Count(sec, `href="/attachments/`+txt.ID+`/notes.txt"`); n != 1 {
		t.Errorf("text file linked %d times, want 1 (row)", n)
	}
	if strings.Contains(sec, "continue") {
		t.Error("stray continue text in the attachments section")
	}
	// Images only: no empty rows list.
	_, c2 := attachEnv(t)
	uploadOK(t, c2.Upload("/t/VAQ-1/attachments", "shot.png", "image/png", pngBytes))
	b2 := c2.Get("/t/VAQ-1").Body
	s2 := b2[strings.Index(b2, `id="attachments"`):]
	s2 = s2[:strings.Index(s2, "data-attach-well")]
	if strings.Contains(s2[strings.Index(s2, "data-gallery"):], "<ul") {
		t.Error("images-only ticket has an empty rows list")
	}
}
