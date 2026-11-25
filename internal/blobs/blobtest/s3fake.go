package blobtest

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// S3FakeBucket is the only bucket name the fake serves.
const S3FakeBucket = "test-bucket"

// S3Fake is an in-memory S3 endpoint speaking just what minio-go sends for
// Put, Get, Delete and a recursive list. Signatures are accepted unchecked.
type S3Fake struct {
	// Endpoint is host:port, for blobs.NewS3 (plain http).
	Endpoint string

	mu      sync.Mutex
	objs    map[string]fakeObj
	failing bool
}

type fakeObj struct {
	body     []byte
	ctype    string
	modified time.Time
}

// NewS3Fake starts a fake and stops it at test cleanup.
func NewS3Fake(t testing.TB) *S3Fake {
	t.Helper()
	f := &S3Fake{objs: map[string]fakeObj{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.Endpoint = strings.TrimPrefix(srv.URL, "http://")
	return f
}

// Fail makes every later request answer 403 AccessDenied (minio-go retries 5xx with backoff, which slows tests).
func (f *S3Fake) Fail() {
	f.mu.Lock()
	f.failing = true
	f.mu.Unlock()
}

func s3Error(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, msg)
}

func (f *S3Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		s3Error(w, http.StatusForbidden, "AccessDenied", "fake failure")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(rest, "/")
	if bucket != S3FakeBucket {
		s3Error(w, http.StatusNotFound, "NoSuchBucket", "no such bucket")
		return
	}
	if key == "" {
		f.list(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		body, err := readBody(r)
		if err != nil {
			s3Error(w, http.StatusBadRequest, "IncompleteBody", err.Error())
			return
		}
		f.objs[key] = fakeObj{body: body, ctype: r.Header.Get("Content-Type"), modified: time.Now().UTC()}
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		o, ok := f.objs[key]
		if !ok {
			s3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
			return
		}
		h := w.Header()
		h.Set("Content-Type", o.ctype)
		h.Set("Content-Length", strconv.Itoa(len(o.body)))
		h.Set("ETag", `"fake"`)
		h.Set("Last-Modified", o.modified.Format(http.TimeFormat))
		if r.Method == http.MethodGet {
			_, _ = w.Write(o.body)
		}
	case http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		s3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "unsupported")
	}
}

func (f *S3Fake) list(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	type content struct {
		Key          string
		LastModified string
		ETag         string
		Size         int
		StorageClass string
	}
	var keys []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := struct {
		XMLName     xml.Name `xml:"ListBucketResult"`
		Name        string
		Prefix      string
		KeyCount    int
		MaxKeys     int
		IsTruncated bool
		Contents    []content
	}{Name: S3FakeBucket, Prefix: prefix, KeyCount: len(keys), MaxKeys: 1000}
	for _, k := range keys {
		o := f.objs[k]
		out.Contents = append(out.Contents, content{k, o.modified.Format("2006-01-02T15:04:05.000Z"), `"fake"`, len(o.body), "STANDARD"})
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(out)
}

// readBody returns the object bytes, decoding aws-chunked streaming bodies
// (what minio-go sends over plain http), ignoring chunk signatures.
func readBody(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out []byte
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("bad chunk header %q", line)
		}
		if n == 0 {
			return out, nil // trailers, if any, are ignored
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := br.Discard(2); err != nil { // CRLF
			return nil, err
		}
	}
}
