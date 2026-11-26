package blobs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

func TestOpenChoosesBackend(t *testing.T) {
	d := dbtest.New(t)
	root := filepath.Join(t.TempDir(), "att")
	s, err := blobs.Open(config.Config{BlobBackend: "sqlite"}, d.W, d.R, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*blobs.SQLite); !ok {
		t.Fatalf("%T", s)
	}
	s, err = blobs.Open(config.Config{BlobBackend: "dir", BlobDir: root}, d.W, d.R, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*blobs.Dir); !ok {
		t.Fatalf("%T", s)
	}
	if fi, err := os.Stat(root); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", fi, err)
	}
	s, err = blobs.Open(config.Config{BlobBackend: "s3", S3Endpoint: "s3.example.com", S3Bucket: "b", S3AccessKeyID: "k", S3SecretAccessKey: []byte("s")}, d.W, d.R, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*blobs.S3); !ok {
		t.Fatalf("%T", s)
	}
	if _, err := blobs.Open(config.Config{BlobBackend: "ftp"}, d.W, d.R, time.Now); err == nil {
		t.Fatal("want an error")
	}
}

// VAQ-122: a blob dir inside the data directory counts as inside when the
// two settings reach it one through a symlink and one by its real path.
func TestLocationSeesThroughSymlinks(t *testing.T) {
	real := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(real, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cases := map[string]config.Config{
		"symlinked data dir": {BlobBackend: "dir", DataDir: link, BlobDir: filepath.Join(real, "attachments")},
		"symlinked blob dir": {BlobBackend: "dir", DataDir: real, BlobDir: filepath.Join(link, "attachments")},
		"both symlinked":     {BlobBackend: "dir", DataDir: link, BlobDir: filepath.Join(link, "attachments")},
		"neither":            {BlobBackend: "dir", DataDir: real, BlobDir: filepath.Join(real, "attachments")},
		"blob dir missing":   {BlobBackend: "dir", DataDir: link, BlobDir: filepath.Join(real, "new")},
	}
	want := map[string]string{"blob dir missing": "{data dir}/new"}
	for name, cfg := range cases {
		w := want[name]
		if w == "" {
			w = "{data dir}/attachments"
		}
		if got := blobs.Location(cfg); got != w {
			t.Errorf("%s: %q, want %q", name, got, w)
		}
	}
}

// VAQ-122: resolving symlinks never changes what an unchanged setup
// records: a dir outside the data directory stays as spelled (absolute,
// cleaned, not resolved), and a symlink inside the data directory that
// points elsewhere still counts as inside.
func TestLocationUnchangedSetups(t *testing.T) {
	data := t.TempDir()
	outside := t.TempDir()
	linkOut := filepath.Join(t.TempDir(), "out")
	if err := os.Symlink(outside, linkOut); err != nil {
		t.Fatal(err)
	}
	if got := blobs.Location(config.Config{BlobBackend: "dir", DataDir: data, BlobDir: linkOut}); got != linkOut {
		t.Fatalf("outside dir recorded %q, want %q", got, linkOut)
	}
	if err := os.Symlink(outside, filepath.Join(data, "attachments")); err != nil {
		t.Fatal(err)
	}
	if got := blobs.Location(config.Config{BlobBackend: "dir", DataDir: data, BlobDir: filepath.Join(data, "attachments")}); got != "{data dir}/attachments" {
		t.Fatalf("inside symlink recorded %q", got)
	}
	if got := blobs.Location(config.Config{BlobBackend: "dir", DataDir: data, BlobDir: filepath.Join(data, "x", "..", "y")}); got != "{data dir}/y" {
		t.Fatalf("%q", got)
	}
}

// VAQ-122: a location recorded before symlinks were resolved (absolute,
// through a symlink) still matches the same directory, so the boot passes.
func TestSameLocation(t *testing.T) {
	real := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(real, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BlobBackend: "dir", DataDir: real, BlobDir: filepath.Join(link, "attachments")}
	for _, rec := range []string{"{data dir}/attachments", filepath.Join(link, "attachments"), filepath.Join(real, "attachments")} {
		if !blobs.SameLocation(rec, cfg) {
			t.Errorf("%q should match", rec)
		}
	}
	other := t.TempDir()
	for _, rec := range []string{other, "{data dir}/elsewhere", ""} {
		if blobs.SameLocation(rec, cfg) {
			t.Errorf("%q should not match", rec)
		}
	}
	s3 := config.Config{BlobBackend: "s3", S3Endpoint: "S3.example.com", S3Bucket: "b"}
	if !blobs.SameLocation("s3.example.com/b", s3) || blobs.SameLocation("s3.example.com/c", s3) {
		t.Fatal("s3 compares by string")
	}
}
