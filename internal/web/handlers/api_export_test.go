package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
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
