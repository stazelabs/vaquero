package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

func TestAPIAttachmentLifecycle(t *testing.T) {
	e, c, tok := apiWorld(t)
	r := c.UploadAs("/api/v1/tickets/vaq-1/attachments", tok, "shot.png", "", pngBytes)
	if r.Status != http.StatusCreated {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var a api.Attachment
	decode(t, r.Body, &a)
	if a.ID == "" || a.Name != "shot.png" || a.ContentType != "image/png" || a.Size != int64(len(pngBytes)) ||
		a.UploadedBy != "Owner" || a.CreatedAt == "" || a.URL != "/attachments/"+a.ID+"/shot.png" {
		t.Fatalf("%+v", a)
	}
	var list []api.Attachment
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1/attachments", tok, nil).Body, &list)
	if len(list) != 1 || list[0] != a {
		t.Fatalf("%+v", list)
	}
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.Attachments) != 1 || tk.Attachments[0] != a {
		t.Fatalf("%+v", tk.Attachments)
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &tk)
	if tk.Attachments == nil || len(tk.Attachments) != 0 {
		t.Fatalf("an empty list is [], got %#v", tk.Attachments)
	}
	if r := c.API("GET", "/api/v1/attachments/"+a.ID, "", nil); r.Status != 401 {
		t.Fatalf("no token: %d", r.Status)
	}
	r = c.API("GET", "/api/v1/attachments/"+strings.ToLower(a.ID), tok, nil)
	if r.Status != 200 || r.Body != string(pngBytes) || r.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
	assertAttachmentHeaders(t, r)
	etag := r.Header.Get("ETag")
	if etag == "" || r.Header.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("etag %q cache %q", etag, r.Header.Get("Cache-Control"))
	}
	if r := c.API("DELETE", "/api/v1/attachments/"+a.ID, tok, nil); r.Status != 204 {
		t.Fatalf("delete %d %s", r.Status, r.Body)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Errorf("bytes left: %v", keys)
	}
	for _, m := range []string{"GET", "DELETE"} {
		r := c.API(m, "/api/v1/attachments/"+a.ID, tok, nil)
		if r.Status != 404 || failMessage(t, r) == "" {
			t.Errorf("%s after delete: %d %s", m, r.Status, r.Body)
		}
	}
}

func TestAPIAttachRefusedForMissingTicket(t *testing.T) {
	e, c, tok := apiWorld(t)
	for _, key := range []string{"VAQ-99", "nope"} {
		r := c.UploadAs("/api/v1/tickets/"+key+"/attachments", tok, "a.txt", "", []byte("hi"))
		if r.Status != 404 || failMessage(t, r) == "" {
			t.Errorf("%s: %d %s", key, r.Status, r.Body)
		}
		if r := c.API("GET", "/api/v1/tickets/"+key+"/attachments", tok, nil); r.Status != 404 {
			t.Errorf("list %s: %d", key, r.Status)
		}
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Errorf("bytes left: %v", keys)
	}
}

func TestAPIUploadTooLarge(t *testing.T) {
	e, c, tok := apiWorld(t)
	r := c.UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "big.bin", "", make([]byte, core.MaxAttachment+1))
	if r.Status != http.StatusRequestEntityTooLarge || failMessage(t, r) != "Files can be at most 10 MB." {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Errorf("bytes left: %v", keys)
	}
	r = c.UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "max.bin", "", make([]byte, core.MaxAttachment))
	if r.Status != http.StatusCreated {
		t.Fatalf("at the limit: %d", r.Status)
	}
}

func TestAPIAttachmentBadUploads(t *testing.T) {
	_, c, tok := apiWorld(t)
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/attachments", tok, map[string]string{"a": "b"}); r.Status != 400 {
		t.Errorf("json body: %d %s", r.Status, r.Body)
	}
	if r := c.UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "empty.txt", "", nil); r.Status != 422 {
		t.Errorf("empty: %d %s", r.Status, r.Body)
	}
}
