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
