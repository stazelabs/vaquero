package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/archive"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core/coreseed"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// exportEnv is a server seeded by coreseed, and the owner's token.
func exportEnv(t *testing.T) (*webtest.Env, string) {
	t.Helper()
	e := webtest.New(t)
	if err := coreseed.Seed(bg, e.Deps.Core, e.DB.Q, e.Deps.Blobs, webtest.Instance, webtest.Start); err != nil {
		t.Fatal(err)
	}
	return e, e.Token(t)
}

func TestAPIExportOwnerOnly(t *testing.T) {
	e, tok := exportEnv(t)
	c := e.Client()
	r := c.API("GET", "/api/v1/export", tok, nil)
	if r.Status != 200 || r.Header.Get("Content-Type") != "application/gzip" ||
		r.Header.Get("Content-Disposition") != `attachment; filename="vaquero-20261118-120000.tar.gz"` {
		t.Fatalf("%d %v %.200q", r.Status, r.Header, r.Body)
	}
	zr, err := gzip.NewReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	h, err := tr.Next()
	if err != nil || h.Name != "export.json" {
		t.Fatalf("%v %v", err, h)
	}
	doc, _ := io.ReadAll(tr)
	if !bytes.HasPrefix(doc, []byte(`{"format":"vaquero-export","format_version":1,"schema":`)) ||
		!bytes.Contains(doc, []byte(`"exported_at":"2026-11-18T12:00:00.000Z"`)) {
		t.Fatalf("%.300s", doc)
	}
	n := 0
	for {
		if _, err := tr.Next(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("%d attachments in the archive", n)
	}

	refused := func(r *webtest.Response) {
		t.Helper()
		var f api.Failure
		decode(t, r.Body, &struct{ Error *api.Failure }{&f})
		if r.Status != 403 || f.Code != "forbidden" || f.Message != "Only the owner can export." {
			t.Fatalf("%d %q", r.Status, r.Body)
		}
	}
	// The owner's token acting as an agent is that agent, not the owner.
	c.Agent = "builder"
	refused(c.API("GET", "/api/v1/export", tok, nil))
	// An agent's own token.
	bot, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "ci-bot")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := e.Deps.Core.CreateToken(bg, e.Deps.Actor.ID, bot.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	c.Agent = ""
	refused(c.API("GET", "/api/v1/export", raw, nil))
	// No token at all.
	if r := c.API("GET", "/api/v1/export", "", nil); r.Status != 401 {
		t.Fatalf("%d", r.Status)
	}
}

// Review Focus 4: a missing file is refused before any byte of the archive.
func TestAPIExportMissingFile(t *testing.T) {
	e, tok := exportEnv(t)
	var id, key string
	if err := e.DB.R.QueryRowContext(bg, "SELECT id, blob_key FROM attachments ORDER BY id LIMIT 1").Scan(&id, &key); err != nil {
		t.Fatal(err)
	}
	if err := e.Deps.Blobs.Delete(bg, key); err != nil {
		t.Fatal(err)
	}
	r := e.Client().API("GET", "/api/v1/export", tok, nil)
	var f api.Failure
	decode(t, r.Body, &struct{ Error *api.Failure }{&f})
	if r.Status != 500 || f.Code != "missing_file" || !strings.Contains(f.Message, id) || !strings.Contains(f.Message, "VAQ-1") ||
		r.Header.Get("Content-Disposition") != "" {
		t.Fatalf("%d %v %q", r.Status, r.Header, r.Body)
	}
}

// HEAD checks as GET does and answers only the headers.
func TestAPIExportHead(t *testing.T) {
	e, tok := exportEnv(t)
	head := func(token, agent string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodHead, e.Server.URL+"/api/v1/export", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if agent != "" {
			req.Header.Set("Vaquero-Agent", agent)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	// Count the store's calls: a HEAD checks each file's size once and
	// reads none; a GET reads each file once.
	counted := &countingStore{Store: e.Deps.Blobs}
	e.Deps.Blobs = counted
	e.Rebuild()
	res := head(tok, "")
	if g, s := counted.gets.Load(), counted.stats.Load(); g != 0 || s != 2 {
		t.Errorf("HEAD made %d reads and %d stats, want 0 and 2 (one per attachment)", g, s)
	}
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/gzip" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="vaquero-20261118-120000.tar.gz"` {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	counted.gets.Store(0)
	if r := e.Client().API("GET", "/api/v1/export", tok, nil); r.Status != 200 {
		t.Fatalf("GET: %d", r.Status)
	}
	if n := counted.gets.Load(); n != 2 {
		t.Errorf("GET read the store %d times, want 2 (each file once)", n)
	}
	if res := head(tok, "builder"); res.StatusCode != 403 {
		t.Fatalf("agent: %d", res.StatusCode)
	}
	var key string
	if err := e.DB.R.QueryRowContext(bg, "SELECT blob_key FROM attachments ORDER BY id LIMIT 1").Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := e.Deps.Blobs.Put(bg, key, strings.NewReader("short"), 5, "image/png"); err != nil {
		t.Fatal(err)
	}
	if res := head(tok, ""); res.StatusCode != 500 || res.Header.Get("Content-Disposition") != "" {
		t.Fatalf("damaged: %d %v", res.StatusCode, res.Header)
	}
	counted.gets.Store(0)
	r := e.Client().API("GET", "/api/v1/export", tok, nil)
	if n := counted.gets.Load(); n != 0 {
		t.Errorf("a refused GET read the store %d times, want 0", n)
	}
	if r.Status != 500 || !strings.Contains(r.Body, `"damaged_file"`) || !strings.Contains(r.Body, "5 bytes in storage") {
		t.Fatalf("%d %q", r.Status, r.Body)
	}
}

type countingStore struct {
	blobs.Store
	gets, stats atomic.Int32
}

func (c *countingStore) Stat(ctx context.Context, key string) (int64, error) {
	c.stats.Add(1)
	return c.Store.Stat(ctx, key)
}

func (c *countingStore) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	c.gets.Add(1)
	return c.Store.Get(ctx, key)
}

// Thumbnails are derived data: an export carries the files, never their
// previews, and export.json never names a thumbnail key.
func TestExportHasNoThumbs(t *testing.T) {
	e, tok := exportEnv(t)
	c := e.Client()
	u := uploadOK(t, c.Upload("/t/VAQ-1/attachments", "wide.png", "", realPNG(t, 600, 300)))
	if !slices.Contains(blobKeys(t, e), thumbKeyOf(u.ID)) {
		t.Fatalf("no thumbnail to leave out: %v", blobKeys(t, e))
	}
	r := c.API("GET", "/api/v1/export", tok, nil)
	if r.Status != 200 {
		t.Fatalf("%d %.200s", r.Status, r.Body)
	}
	zr, err := gzip.NewReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		b, _ := io.ReadAll(tr)
		if h.Name == "export.json" && bytes.Contains(b, []byte("thumbs/")) {
			t.Error("export.json names a thumbnail key")
		}
	}
	if len(names) != 4 || !slices.Contains(names, "attachments/"+u.ID) {
		t.Fatalf("want export.json and three files: %v", names)
	}
	for _, n := range names {
		if strings.Contains(n, "thumb") {
			t.Errorf("thumbnail in the archive: %s", n)
		}
	}
}

// VAQ-132: past archive.MaxDocument, GET and HEAD refuse with the same
// message before any byte of the archive, and read no file.
func TestAPIExportTooLarge(t *testing.T) {
	e, tok := exportEnv(t)
	saved := archive.MaxDocument
	archive.MaxDocument = 1000
	t.Cleanup(func() { archive.MaxDocument = saved })
	counted := &countingStore{Store: e.Deps.Blobs}
	e.Deps.Blobs = counted
	e.Rebuild()
	r := e.Client().API("GET", "/api/v1/export", tok, nil)
	var f api.Failure
	decode(t, r.Body, &struct{ Error *api.Failure }{&f})
	if r.Status != 500 || f.Code != "export_too_large" || !strings.Contains(f.Message, "vaquero import") ||
		r.Header.Get("Content-Disposition") != "" || counted.gets.Load() != 0 {
		t.Fatalf("GET: %d %v %q, %d reads", r.Status, r.Header, r.Body, counted.gets.Load())
	}
	req, _ := http.NewRequest(http.MethodHead, e.Server.URL+"/api/v1/export", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 500 || res.Header.Get("Content-Disposition") != "" {
		t.Fatalf("HEAD: %d %v", res.StatusCode, res.Header)
	}
}
