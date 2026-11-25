package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// pngBytes starts with the PNG signature, so it sniffs as image/png.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{7}, 64)...)

type uploaded struct {
	ID, Name, URL, Markdown string
	Image                   bool
}

func uploadOK(t *testing.T, r *webtest.Response) uploaded {
	t.Helper()
	if r.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}
	var u uploaded
	if err := json.Unmarshal([]byte(r.Body), &u); err != nil {
		t.Fatalf("%v: %s", err, r.Body)
	}
	return u
}

func failMessage(t *testing.T, r *webtest.Response) string {
	t.Helper()
	var f struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(r.Body), &f); err != nil {
		t.Fatalf("not the failure envelope: %v: %s", err, r.Body)
	}
	return f.Error.Message
}

func blobKeys(t *testing.T, e *webtest.Env) []string {
	t.Helper()
	var keys []string
	if err := e.Deps.Blobs.List(bg, "", func(k string, _ time.Time) error { keys = append(keys, k); return nil }); err != nil {
		t.Fatal(err)
	}
	return keys
}

func attachEnv(t *testing.T) (*webtest.Env, *webtest.Client) {
	t.Helper()
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Paste here"})
	return e, e.Client()
}

func assertAttachmentHeaders(t *testing.T, r *webtest.Response) {
	t.Helper()
	if got := r.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff: %q", got)
	}
	if got := r.Header.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "sandbox" {
		t.Errorf("csp: %q", got)
	}
}

func TestUploadAndServeImage(t *testing.T) {
	e, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "image/png", pngBytes))
	if !u.Image || u.Name != "shot.png" || u.URL != "/attachments/"+u.ID+"/shot.png" || u.Markdown != "![shot.png]("+u.URL+")" {
		t.Fatalf("%+v", u)
	}
	a, err := e.Deps.R.GetAttachment(bg, u.ID)
	if err != nil || a.ContentType != "image/png" || a.Size != int64(len(pngBytes)) ||
		a.BlobKey != "attachments/"+webtest.Instance+"/"+strings.ToLower(u.ID) {
		t.Fatalf("%+v %v", a, err)
	}
	r := c.Get(u.URL)
	if r.Status != 200 || r.Body != string(pngBytes) || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Errorf("disposition %q", cd)
	}
	assertAttachmentHeaders(t, r)
	if h := c.Head(u.URL); h.Status != 200 {
		t.Errorf("head %d", h.Status)
	}
}

func TestUploadTooLarge(t *testing.T) {
	e, c := attachEnv(t)
	r := c.Upload("/t/VAQ-1/attachments", "big.bin", "", make([]byte, core.MaxAttachment+1))
	if r.Status != http.StatusRequestEntityTooLarge || failMessage(t, r) != "Files can be at most 10 MB." {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Fatalf("stored %v", keys)
	}
	// A body of unknown length past the upload limit is cut off while it
	// is read, and still answers with the envelope.
	var b bytes.Buffer
	b.WriteString("--x\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n\r\n")
	b.Write(make([]byte, core.MaxAttachment+200<<10))
	b.WriteString("\r\n--x--\r\n")
	req := httptest.NewRequest(http.MethodPost, "/t/VAQ-1/attachments", &b)
	req.Host = "127.0.0.1"
	req.ContentLength = -1
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	rec := httptest.NewRecorder()
	e.Server.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "Files can be at most 10 MB.") {
		t.Fatalf("unknown length: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUploadAtLimit(t *testing.T) {
	e, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "notes.txt", "", bytes.Repeat([]byte("a"), core.MaxAttachment)))
	if u.Image || u.Markdown != "[notes.txt]("+u.URL+")" {
		t.Fatalf("%+v", u)
	}
	if a, _ := e.Deps.R.GetAttachment(bg, u.ID); a.Size != core.MaxAttachment {
		t.Fatalf("size %d", a.Size)
	}
	if r := c.Get(u.URL); r.Status != 200 || len(r.Body) != core.MaxAttachment {
		t.Fatalf("%d %d", r.Status, len(r.Body))
	}
}

// Review Focus 2: the type comes from the bytes, never from the name or the
// part's declared type, and only a real image is served inline.
func TestDisguisedUploadServedAsDownload(t *testing.T) {
	e, c := attachEnv(t)
	for _, tc := range []struct{ name, ctype, body, stored string }{
		{"shot.png", "image/png", "<!DOCTYPE html><html><script>alert(1)</script></html>", "text/html; charset=utf-8"},
		{"logo.png", "image/png", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, "text/xml; charset=utf-8"},
		{"logo.svg", "image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, "text/plain; charset=utf-8"},
	} {
		u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", tc.name, tc.ctype, []byte(tc.body)))
		if u.Image || strings.HasPrefix(u.Markdown, "!") {
			t.Errorf("%s: answered as an image: %+v", tc.name, u)
		}
		if a, _ := e.Deps.R.GetAttachment(bg, u.ID); a.ContentType != tc.stored {
			t.Errorf("%s: stored as %q, want %q", tc.name, a.ContentType, tc.stored)
		}
		r := c.Get(u.URL)
		if r.Status != 200 || r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s: %d %q", tc.name, r.Status, r.Header.Get("Content-Type"))
		}
		if cd := r.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=UTF-8''"+tc.name) {
			t.Errorf("%s: disposition %q", tc.name, cd)
		}
		assertAttachmentHeaders(t, r)
	}
}

// Review Focus 3: awkward names round-trip through the URL, and a wrong
// name redirects once to the canonical URL, which never redirects again.
func TestAttachmentURLRoundTrip(t *testing.T) {
	_, c := attachEnv(t)
	for _, tc := range []struct{ name, data, escaped, star string }{
		{"a b#?%.png", string(pngBytes), "a%20b%23%3F%25.png", "a%20b%23%3F%25.png"},
		{"résumé 📎.pdf", "%PDF-1.4\n", "r%C3%A9sum%C3%A9%20%F0%9F%93%8E.pdf", "r%C3%A9sum%C3%A9%20%F0%9F%93%8E.pdf"},
		{"x(1) [draft].txt", "plain", "x%281%29%20%5Bdraft%5D.txt", "x%281%29%20%5Bdraft%5D.txt"},
	} {
		u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", tc.name, "", []byte(tc.data)))
		if u.Name != tc.name || u.URL != "/attachments/"+u.ID+"/"+tc.escaped {
			t.Fatalf("%q: %+v", tc.name, u)
		}
		ok := c.Get(u.URL)
		if ok.Status != 200 || ok.Body != tc.data {
			t.Fatalf("%q: canonical %d %q", tc.name, ok.Status, ok.Location)
		}
		assertAttachmentHeaders(t, ok)
		if cd := ok.Header.Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''"+tc.star) {
			t.Errorf("%q: disposition %q", tc.name, cd)
		}
		for _, wrong := range []string{"/attachments/" + u.ID + "/wrong.png", "/attachments/" + strings.ToLower(u.ID) + "/" + tc.escaped, "/attachments/" + u.ID + "/" + url.PathEscape("a/b")} {
			r := c.Get(wrong)
			if r.Status != http.StatusMovedPermanently || r.Location != u.URL {
				t.Fatalf("%q: %s answered %d %q", tc.name, wrong, r.Status, r.Location)
			}
			assertAttachmentHeaders(t, r)
			if again := c.Follow(r); again.Status != 200 {
				t.Fatalf("%q: followed %d %q", tc.name, again.Status, again.Location)
			}
		}
	}
	// The markdown names a link label safely too.
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", `a]b[c.txt`, "", []byte("x")))
	if u.Markdown != `[a\]b\[c.txt](`+u.URL+")" {
		t.Fatalf("%q", u.Markdown)
	}
}

func TestUploadRefusedLeavesNoBlob(t *testing.T) {
	e, c := attachEnv(t)
	// Core refuses an empty file after the bytes were stored: the handler
	// deletes them.
	r := c.Upload("/t/VAQ-1/attachments", "empty.txt", "", nil)
	if r.Status != http.StatusUnprocessableEntity || failMessage(t, r) != "That file is empty." {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.Upload("/t/VAQ-9/attachments", "shot.png", "", pngBytes)
	if r.Status != http.StatusNotFound || failMessage(t, r) != "No ticket VAQ-9." {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.Post("/t/VAQ-1/attachments", url.Values{"file": {"x"}})
	if r.Status != http.StatusBadRequest || failMessage(t, r) == "" {
		t.Fatalf("not multipart: %d %s", r.Status, r.Body)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Fatalf("stored %v", keys)
	}
	if n, _ := e.Deps.R.CountAttachments(bg); n != 0 {
		t.Fatalf("%d rows", n)
	}
}

func TestDeleteAttachmentRemovesBytes(t *testing.T) {
	e, c := attachEnv(t)
	ticket(t, e, mustProject(t, e), core.TicketInput{Title: "Other"})
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "", pngBytes))
	// The id must belong to the ticket in the URL.
	r := c.Post("/t/VAQ-2/attachments/"+u.ID+"/delete", nil)
	if r.Status != 303 || r.Location != "/t/VAQ-2" || len(blobKeys(t, e)) != 1 {
		t.Fatalf("other ticket: %d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "That attachment no longer exists.")
	r = c.Post("/t/VAQ-1/attachments/"+u.ID+"/delete", nil)
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Attachment removed.")
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Fatalf("bytes left: %v", keys)
	}
	gone := c.Get(u.URL)
	if gone.Status != http.StatusNotFound {
		t.Fatalf("%d", gone.Status)
	}
	assertAttachmentHeaders(t, gone)
}
