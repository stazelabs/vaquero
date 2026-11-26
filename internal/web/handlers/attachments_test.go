package handlers_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/handlers"
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
			if r.Status != http.StatusFound || r.Location != u.URL {
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

// Served bytes are revalidated on every use: the id is the ETag, and a
// matching If-None-Match gets a bodiless 304 with the same security headers.
func TestAttachmentRevalidated(t *testing.T) {
	e, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "", pngBytes))
	r := c.Get(u.URL)
	etag := r.Header.Get("ETag")
	if etag != `"`+u.ID+`"` || r.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("etag %q cache %q", etag, r.Header.Get("Cache-Control"))
	}
	for _, inm := range []string{etag, `"other", W/` + etag, "*"} {
		req, _ := http.NewRequest(http.MethodGet, e.Server.URL+u.URL, nil)
		req.Header.Set("If-None-Match", inm)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotModified || res.Header.Get("ETag") != etag ||
			res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Content-Security-Policy") != "sandbox" {
			t.Fatalf("%q: %d %v", inm, res.StatusCode, res.Header)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, e.Server.URL+u.URL, nil)
	req.Header.Set("If-None-Match", `"other"`)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("other etag: %d", res.StatusCode)
	}
}

// realPNG is a w x h opaque PNG that decodes.
func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func thumbKeyOf(id string) string { return "thumbs/" + webtest.Instance + "/" + strings.ToLower(id) }

func hasKey(t *testing.T, e *webtest.Env, key string) bool {
	t.Helper()
	return slices.Contains(blobKeys(t, e), key)
}

func TestThumbRouteMakesOnFirstRequestThen304(t *testing.T) {
	e, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "wide.png", "", realPNG(t, 800, 400)))
	// The upload made it.
	if !hasKey(t, e, thumbKeyOf(u.ID)) {
		t.Fatalf("no thumbnail after upload: %v", blobKeys(t, e))
	}
	// After an import there is none: the first request makes and keeps it.
	if err := e.Deps.Blobs.Delete(bg, thumbKeyOf(u.ID)); err != nil {
		t.Fatal(err)
	}
	thumb := "/thumbs/" + u.ID
	r := c.Get(thumb)
	if r.Status != 200 || r.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
	assertAttachmentHeaders(t, r)
	etag := r.Header.Get("ETag")
	if etag != `"`+u.ID+`-t1"` || r.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("etag %q cache %q", etag, r.Header.Get("Cache-Control"))
	}
	cfg, format, err := image.DecodeConfig(strings.NewReader(r.Body))
	if err != nil || format != "jpeg" || cfg.Width != 192 || cfg.Height != 96 {
		t.Fatalf("%+v %s %v", cfg, format, err)
	}
	if !hasKey(t, e, thumbKeyOf(u.ID)) {
		t.Fatal("the made thumbnail wasn't kept")
	}
	// The kept one is served again, byte for byte.
	if again := c.Get(thumb); again.Status != 200 || again.Body != r.Body {
		t.Fatalf("second request: %d", again.Status)
	}
	req, _ := http.NewRequest(http.MethodGet, e.Server.URL+thumb, nil)
	req.Header.Set("If-None-Match", etag)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotModified || res.Header.Get("ETag") != etag ||
		res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	// Another case of the id goes to the canonical address.
	if r := c.Get("/thumbs/" + strings.ToLower(u.ID)); r.Status != 302 || r.Location != thumb {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	if r := c.Head(thumb); r.Status != 200 {
		t.Fatalf("head %d", r.Status)
	}
}

// Review Focus 3: what can't be previewed falls back to the original, and
// no thumbnail is stored for it.
func TestThumbFallsBackToOriginal(t *testing.T) {
	e, c := attachEnv(t)
	ihdr := []byte("IHDR\x00\x00\x27\x10\x00\x00\x13\x88\x08\x06\x00\x00\x00") // 10000 x 5000
	huge := append(append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0d"), ihdr...), binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(ihdr))...)
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00" + strings.Repeat("\x00", 24))
	full := realPNG(t, 300, 300)
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"bogus.png", pngBytes},          // sniffs as PNG, isn't one
		{"huge.png", huge},               // 50 megapixels
		{"photo.webp", webp},             // no standard decoder
		{"cut.png", full[:len(full)/2]},  // truncated
		{"notes.txt", []byte("words\n")}, // not an image at all
	} {
		u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", f.name, "", f.data))
		r := c.Get("/thumbs/" + u.ID)
		if r.Status != http.StatusFound || r.Location != u.URL {
			t.Errorf("%s: %d %q", f.name, r.Status, r.Location)
		}
		assertAttachmentHeaders(t, r)
		if hasKey(t, e, thumbKeyOf(u.ID)) {
			t.Errorf("%s: a thumbnail was stored", f.name)
		}
	}
	// Both the preview and the original gone: the 302 leads to a 404.
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "lost.png", "", realPNG(t, 300, 300)))
	for _, k := range []string{thumbKeyOf(u.ID), "attachments/" + webtest.Instance + "/" + strings.ToLower(u.ID)} {
		if err := e.Deps.Blobs.Delete(bg, k); err != nil {
			t.Fatal(err)
		}
	}
	r := c.Get("/thumbs/" + u.ID)
	if r.Status != http.StatusFound || r.Location != u.URL {
		t.Fatalf("both gone: %d %q", r.Status, r.Location)
	}
	assertAttachmentHeaders(t, r)
	if r := c.Follow(r); r.Status != http.StatusNotFound {
		t.Fatalf("followed: %d", r.Status)
	}
	if r := c.Get("/thumbs/01JZZZZZZZZZZZZZZZZZZZZZZ9"); r.Status != http.StatusNotFound {
		t.Fatalf("missing: %d", r.Status)
	}
}

func TestDeletingAttachmentDeletesThumb(t *testing.T) {
	e, c := attachEnv(t)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "a.png", "", realPNG(t, 400, 400)))
	if keys := blobKeys(t, e); len(keys) != 2 || !hasKey(t, e, thumbKeyOf(u.ID)) {
		t.Fatalf("want the file and its thumbnail: %v", keys)
	}
	if r := c.Post("/t/VAQ-1/attachments/"+u.ID+"/delete", nil); r.Status != 303 {
		t.Fatalf("%d", r.Status)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Fatalf("left after the web delete: %v", keys)
	}
	if r := c.Get("/thumbs/" + u.ID); r.Status != http.StatusNotFound {
		t.Fatalf("thumb after delete: %d", r.Status)
	}
	// The API's delete too.
	tok := e.Token(t)
	u = uploadOK(t, c.Upload("/t/VAQ-1/attachments", "b.png", "", realPNG(t, 400, 400)))
	if r := c.API("DELETE", "/api/v1/attachments/"+u.ID, tok, nil); r.Status != http.StatusNoContent {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if keys := blobKeys(t, e); len(keys) != 0 {
		t.Fatalf("left after the API delete: %v", keys)
	}
}

// The ticket page shows images first, as previews linking to the original
// in a new tab, then every other file as a row.
func TestTicketPageGallery(t *testing.T) {
	_, c := attachEnv(t)
	doc := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "notes.txt", "", []byte("words\n")))
	img := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "shot.png", "", realPNG(t, 300, 200)))
	body := c.Get("/t/VAQ-1").Body
	contains(t, body, `data-gallery`, `src="/thumbs/`+img.ID+`"`, `width="96"`, `height="96"`, `loading="lazy"`,
		`href="`+img.URL+`" target="_blank"`, `href="`+doc.URL+`"`)
	if strings.Index(body, img.URL) > strings.Index(body, doc.URL) {
		t.Error("the image should come before the other file")
	}
	lacks(t, body, `src="/attachments/`+doc.ID, `src="/thumbs/`+doc.ID)
}

// An image named "thumb" keeps its own address: the original is served
// there, not a preview.
func TestImageNamedThumbKeepsItsURL(t *testing.T) {
	_, c := attachEnv(t)
	data := realPNG(t, 400, 400)
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "thumb", "", data))
	if u.URL != "/attachments/"+u.ID+"/thumb" {
		t.Fatalf("%q", u.URL)
	}
	r := c.Get(u.URL)
	if r.Status != 200 || r.Body != string(data) || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("ETag") != `"`+u.ID+`"` {
		t.Fatalf("%d %q %q", r.Status, r.Header.Get("Content-Type"), r.Header.Get("ETag"))
	}
	if p := c.Get("/thumbs/" + u.ID); p.Status != 200 || p.Body == string(data) {
		t.Fatalf("preview: %d", p.Status)
	}
}

// Making a preview that runs out of time (here, stuck waiting for its
// turn) never fails the upload, and the preview route falls back to the
// original; the next view makes it.
func TestThumbOutOfTimeFallsBack(t *testing.T) {
	e, c := attachEnv(t)
	var waited atomic.Int32
	handlers.SetPreviewMaker(t, 30*time.Millisecond, func(ctx context.Context, _ io.Reader) ([]byte, string, error) {
		waited.Add(1)
		<-ctx.Done()
		return nil, "", ctx.Err()
	})
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "slow.png", "", realPNG(t, 300, 300)))
	r := c.Get("/thumbs/" + u.ID)
	if r.Status != http.StatusFound || r.Location != u.URL || waited.Load() != 2 {
		t.Fatalf("%d %q, made %d times", r.Status, r.Location, waited.Load())
	}
	if hasKey(t, e, thumbKeyOf(u.ID)) {
		t.Fatal("a thumbnail was stored")
	}
}
