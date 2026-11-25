package blobs

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 is a Store on an S3-compatible bucket (Cloudflare R2, Tigris, MinIO,
// AWS) through minio-go.
//
// The endpoint is a host with an optional port, spoken to over TLS. Bucket
// addressing is minio's auto mode, which picks virtual-host or path style by
// endpoint (path style for an IP such as a test fake). Region defaults to
// "auto", which R2 expects, and which also stops the client from asking the
// server for the bucket's location first.
//
// The client uses a plain http.Transport with sane timeouts: the host is
// operator configuration, not user data, like the Resend and model clients.
//
// Credentials never appear in an error or a log: every error is wrapped as
// "s3 <op> <key>: <minio error>", and minio's errors carry the server's
// message and request ids, never the secret.
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 opens a store on bucket at endpoint.
func NewS3(endpoint, bucket, accessKey, secretKey, region string) (*S3, error) {
	return newS3(endpoint, bucket, accessKey, secretKey, region, true)
}

func newS3(endpoint, bucket, accessKey, secretKey, region string, secure bool) (*S3, error) {
	if region == "" {
		region = "auto"
	}
	if endpoint == "" || bucket == "" {
		return nil, fmt.Errorf("blobs: s3 needs an endpoint and a bucket")
	}
	c, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       secure,
		Region:       region,
		BucketLookup: minio.BucketLookupAuto,
		Transport:    transport(),
	})
	if err != nil {
		// minio.New's errors name the endpoint at most, never the keys.
		return nil, fmt.Errorf("blobs: s3 client: %w", err)
	}
	return &S3{client: c, bucket: bucket}, nil
}

func transport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
}

func isNoSuchKey(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == minio.NoSuchKey
}

// Put uploads size bytes (-1 streams with unknown length).
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

// Get opens the object; NoSuchKey is ErrNotFound.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if err := ValidKey(key); err != nil {
		return nil, 0, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("s3 get %s: %w", key, err)
	}
	st, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if isNoSuchKey(err) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, fmt.Errorf("s3 get %s: %w", key, err)
	}
	return obj, st.Size, nil
}

// Stat asks for the object's size (a HEAD request); NoSuchKey is
// ErrNotFound.
func (s *S3) Stat(ctx context.Context, key string) (int64, error) {
	if err := ValidKey(key); err != nil {
		return 0, err
	}
	st, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNoSuchKey(err) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("s3 stat %s: %w", key, err)
	}
	return st.Size, nil
}

// Delete removes the object; NoSuchKey is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	if err := ValidKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil && !isNoSuchKey(err) {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

// List calls fn for every object under the directory prefix. "text" and
// "text/" both mean the keys under "text/", never "textfoo/...".
func (s *S3) List(ctx context.Context, prefix string, fn func(key string, modified time.Time) error) error {
	dir, err := validPrefix(prefix)
	if err != nil {
		return err
	}
	if dir != "" {
		dir += "/"
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: dir, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("s3 list %s: %w", prefix, obj.Err)
		}
		if err := fn(obj.Key, obj.LastModified); err != nil {
			return err
		}
	}
	return nil
}
