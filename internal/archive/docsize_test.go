package archive_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/archive"
)

// setMaxDocument lowers archive.MaxDocument for one test.
func setMaxDocument(t *testing.T, n int64) {
	t.Helper()
	saved := archive.MaxDocument
	archive.MaxDocument = n
	t.Cleanup(func() { archive.MaxDocument = saved })
}

// VAQ-132, Review Focus 4: an export.json of exactly MaxDocument bytes is
// written and read back; one byte more is refused by Write and Check
// before anything reaches the writer, so export never writes an archive
// import refuses.
func TestExportDocumentAtTheLimit(t *testing.T) {
	d, store := seeded(t)
	full := export(t, d, store)
	doc := int64(len(unpack(t, full)[0].data))

	setMaxDocument(t, doc)
	if err := archive.Check(bg, d.R, store, header); err != nil {
		t.Fatalf("Check at the limit: %v", err)
	}
	at := export(t, d, store)
	rd, err := archive.Read(bytes.NewReader(at), t.TempDir(), 1)
	if err != nil {
		t.Fatalf("import refuses what export wrote at the limit: %v", err)
	}
	rd.Close()

	setMaxDocument(t, doc-1)
	var buf bytes.Buffer
	err = archive.Write(bg, &buf, d.R, store, header)
	tooLarge, ok := errors.AsType[*archive.TooLarge](err)
	if !ok {
		t.Fatalf("Write past the limit: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Write wrote %d bytes before refusing", buf.Len())
	}
	if !strings.Contains(tooLarge.Error(), "export.json") || !strings.Contains(tooLarge.Error(), "vaquero import") {
		t.Fatalf("message %q", tooLarge.Error())
	}
	if _, ok := errors.AsType[*archive.TooLarge](archive.Check(bg, d.R, store, header)); !ok {
		t.Fatal("Check past the limit passes")
	}
	// What Write refuses, Read refuses too: the limits are the same.
	if _, err := archive.Read(bytes.NewReader(at), t.TempDir(), 1); err == nil {
		t.Fatal("Read takes an export.json over the limit")
	}
}

// A limit smaller than the header alone is refused too.
func TestCheckRefusesTinyLimit(t *testing.T) {
	d, store := seeded(t)
	setMaxDocument(t, 10)
	if _, ok := errors.AsType[*archive.TooLarge](archive.Check(bg, d.R, store, header)); !ok {
		t.Fatal("Check passes a 10-byte limit")
	}
}
