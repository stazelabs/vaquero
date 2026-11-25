package blobs_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/blobs/blobtest"
)

const fakeSecret = "s3cr3t-key-DO-NOT-LEAK"

func newFakeS3(t *testing.T) (*blobs.S3, *blobtest.S3Fake) {
	t.Helper()
	f := blobtest.NewS3Fake(t)
	s, err := blobs.NewS3Plain(f.Endpoint, blobtest.S3FakeBucket, "AKIATEST", fakeSecret, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func TestS3(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blobs.Store {
		s, _ := newFakeS3(t)
		return s
	})
}

func TestS3PrefixIsADirectory(t *testing.T) {
	s, _ := newFakeS3(t)
	ctx := context.Background()
	for _, k := range []string{"images/a.png", "imagesfoo/b.png"} {
		if err := s.Put(ctx, k, strings.NewReader("x"), 1, "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := s.List(ctx, "images", func(k string, _ time.Time) error { got = append(got, k); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "images/a.png" {
		t.Fatalf("got %v", got)
	}
}

func TestS3ErrorsDoNotCarryTheSecret(t *testing.T) {
	s, f := newFakeS3(t)
	ctx := context.Background()
	f.Fail()
	errs := []error{
		s.Put(ctx, "a", strings.NewReader("x"), 1, "x/y"),
		s.Delete(ctx, "a"),
		s.List(ctx, "", func(string, time.Time) error { return nil }),
	}
	_, _, gerr := s.Get(ctx, "a")
	errs = append(errs, gerr)
	for i, err := range errs {
		if err == nil {
			t.Fatalf("op %d succeeded against a failing server", i)
		}
		if errors.Is(err, blobs.ErrNotFound) {
			t.Fatalf("op %d: server failure read as not found", i)
		}
		if strings.Contains(err.Error(), fakeSecret) {
			t.Fatalf("op %d error carries the secret: %v", i, err)
		}
		if !strings.HasPrefix(err.Error(), "s3 ") {
			t.Fatalf("op %d error not wrapped: %v", i, err)
		}
	}
}

func TestS3Constructor(t *testing.T) {
	if _, err := blobs.NewS3("", "b", "a", "s", ""); err == nil {
		t.Error("empty endpoint accepted")
	}
	if _, err := blobs.NewS3("example.com", "", "a", "s", ""); err == nil {
		t.Error("empty bucket accepted")
	}
	if _, err := blobs.NewS3("example.com", "b", "a", "s", "auto"); err != nil {
		t.Errorf("valid config: %v", err)
	}
	if _, err := blobs.NewS3("http://bad/path", "b", "a", "s", ""); err == nil {
		t.Error("endpoint with scheme accepted")
	}
}

func TestS3ListStopsOnCallbackError(t *testing.T) {
	s, _ := newFakeS3(t)
	ctx := context.Background()
	for _, k := range []string{"a/1", "a/2", "a/3"} {
		_ = s.Put(ctx, k, strings.NewReader("x"), 1, "x/y")
	}
	stop := errors.New("stop")
	if err := s.List(ctx, "a", func(string, time.Time) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("got %v", err)
	}
}
