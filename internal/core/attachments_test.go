package core_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/idgen"
)

func attach(c *core.Core, by, ticket, name string) (string, error) {
	a, err := c.AddAttachment(bg, by, core.AttachmentInput{TicketID: ticket, Name: name, ContentType: "image/png", Size: 10, BlobKey: "attachments/i/" + idgen.New()})
	return a.ID, err
}

func init() {
	ops = append(ops,
		coreOp{"AddAttachment",
			func(c *core.Core, f fixture) error { _, err := attach(c, f.Owner.ID, f.T1.ID, "shot.png"); return err },
			func(c *core.Core, f fixture) error { _, err := attach(c, f.Owner.ID, "nope", "shot.png"); return err }},
		coreOp{"DeleteAttachment",
			func(c *core.Core, f fixture) error {
				// Arranged without an event, so the operation's is the only one.
				const id = "01ATT"
				if _, err := c.DB.Exec(`INSERT INTO attachments (id, ticket_id, uploaded_by, name, content_type, size, blob_key, created_at)
					VALUES (?, ?, ?, 'a.png', 'image/png', 1, 'attachments/i/01att', '2026-11-18T12:00:00Z')`, id, f.T1.ID, f.Owner.ID); err != nil {
					return err
				}
				_, err := c.DeleteAttachment(bg, f.Owner.ID, id)
				return err
			},
			func(c *core.Core, f fixture) error { _, err := c.DeleteAttachment(bg, f.Owner.ID, "nope"); return err }},
	)
	// The attachment row is written, then the event refuses.
	lateFail["AddAttachment"] = func(c *core.Core, f fixture, snap func()) error {
		snap()
		_, err := attach(c, f.Owner.ID, f.T1.ID, "shot.png")
		return err
	}
	// The row is deleted, then the event refuses.
	lateFail["DeleteAttachment"] = func(c *core.Core, f fixture, snap func()) error {
		id, err := attach(c, f.Owner.ID, f.T1.ID, "a.png")
		if err != nil {
			return err
		}
		snap()
		_, err = c.DeleteAttachment(bg, f.Owner.ID, id)
		return err
	}
}

func TestAddAttachmentEmitsOneEvent(t *testing.T) {
	c, d, f := newFixture(t)
	before, _ := d.RQ.MaxEventSeq(bg)
	id, err := attach(c, f.Owner.ID, f.T1.ID, "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := d.RQ.MaxEventSeq(bg); after != before+1 {
		t.Fatalf("events: %d", after-before)
	}
	p := lastPayload(t, c)
	if p["attachment_id"] != id || p["name"] != "shot.png" {
		t.Fatalf("%v", p)
	}
	for in, msg := range map[int64]string{0: "That file is empty.", core.MaxAttachment + 1: "Files can be at most 10 MB."} {
		_, err := c.AddAttachment(bg, f.Owner.ID, core.AttachmentInput{TicketID: f.T1.ID, Name: "x", Size: in, BlobKey: "k"})
		fieldErr(t, err, "file", msg)
	}
	if _, err := c.AddAttachment(bg, f.Owner.ID, core.AttachmentInput{TicketID: f.T1.ID, Name: "x", Size: core.MaxAttachment, BlobKey: "k2"}); err != nil {
		t.Fatalf("exactly the limit: %v", err)
	}
}

func TestDeleteAttachmentOnlyUploaderOrOwner(t *testing.T) {
	c, d, f := newFixture(t)
	a1, _ := c.EnsureAgent(bg, f.Owner.ID, "one")
	a2, _ := c.EnsureAgent(bg, f.Owner.ID, "two")
	id, err := attach(c, a1.ID, f.T1.ID, "a.png")
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	_, err = c.DeleteAttachment(bg, a2.ID, id)
	fieldErr(t, err, "attachment", "You can only remove your own attachments.")
	if got := snapshot(t, d); got["attachments"] != snap["attachments"] {
		t.Fatal("refused delete changed rows")
	}
	key, err := c.DeleteAttachment(bg, a1.ID, id) // the uploader
	if err != nil || !strings.HasPrefix(key, "attachments/i/") {
		t.Fatalf("%q %v", key, err)
	}
	id2, _ := attach(c, a1.ID, f.T1.ID, "b.png")
	if _, err := c.DeleteAttachment(bg, f.Owner.ID, id2); err != nil { // the owner
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM attachments`); n != 0 {
		t.Fatalf("%d left", n)
	}
	if p := lastPayload(t, c); p["attachment_id"] != id2 || p["name"] != "b.png" {
		t.Fatalf("%v", p)
	}
}

func TestAttachmentNameCleaned(t *testing.T) {
	long := strings.Repeat("é", 300) + ".png"
	for in, want := range map[string]string{
		"../../etc/passwd":   "passwd",
		`C:\dir\shot.png`:    "shot.png",
		"a\u202eexe.png":     "aexe.png",
		"a\x00b\x1f.txt":     "ab.txt",
		"  .  ":              "file",
		"":                   "file",
		"dir/":               "file",
		" ..name.. ":         "name",
		"my shot #1 (2).png": "my shot #1 (2).png",
		"😀 party.png":        "😀 party.png",
	} {
		if got := core.CleanName(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	got := core.CleanName(long)
	if n := len([]rune(got)); n != 200 || !strings.HasSuffix(got, ".png") {
		t.Fatalf("%d runes, %q", n, got[len(got)-10:])
	}
	if got := core.CleanName(strings.Repeat("é", 300)); len([]rune(got)) != 200 {
		t.Fatalf("no extension: %d runes", len([]rune(got)))
	}
	c, d, f := newFixture(t)
	if _, err := attach(c, f.Owner.ID, f.T1.ID, "../x/evil.png"); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := d.R.QueryRow(`SELECT name FROM attachments`).Scan(&name); err != nil || name != "evil.png" {
		t.Fatalf("%q %v", name, err)
	}
}

func TestFirstAttachmentRecordsBackend(t *testing.T) {
	c, d, f := newFixture(t)
	c.BlobStore = "dir"
	c.BlobLocation = "/srv/attachments"
	if _, err := d.RQ.GetSetting(bg, "blob_store"); err == nil {
		t.Fatal("set before any attachment")
	}
	if _, err := attach(c, f.Owner.ID, f.T1.ID, "a.png"); err != nil {
		t.Fatal(err)
	}
	if s, err := d.RQ.GetSetting(bg, "blob_store"); err != nil || s.Value != "dir" {
		t.Fatalf("%+v %v", s, err)
	}
	if s, err := d.RQ.GetSetting(bg, "blob_location"); err != nil || s.Value != "/srv/attachments" {
		t.Fatalf("location %+v %v", s, err)
	}
	c.BlobStore = "s3" // an existing setting is never overwritten here
	if _, err := attach(c, f.Owner.ID, f.T1.ID, "b.png"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.RQ.GetSetting(bg, "blob_store"); s.Value != "dir" {
		t.Fatalf("%+v", s)
	}
}
