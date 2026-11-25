package blobs_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
)

func TestValidKey(t *testing.T) {
	for _, k := range []string{"a", "text/x.txt", "a-b_c.d/e", strings.Repeat("a", blobs.MaxKeyLen)} {
		if err := blobs.ValidKey(k); err != nil {
			t.Errorf("ValidKey(%q) = %v", k, err)
		}
	}
	for _, k := range []string{"", "a//b", "A", "..", "a/.b", strings.Repeat("a", blobs.MaxKeyLen+1)} {
		if blobs.ValidKey(k) == nil {
			t.Errorf("ValidKey(%q) accepted", k)
		}
	}
}

func TestPutTimeoutScalesWithSize(t *testing.T) {
	for size, want := range map[int64]time.Duration{
		-1:          10 * time.Second,
		0:           10 * time.Second,
		1:           11 * time.Second,
		256 << 10:   11 * time.Second,
		256<<10 + 1: 12 * time.Second,
		10 << 20:    50 * time.Second, // the largest attachment
	} {
		if got := blobs.PutTimeout(size); got != want {
			t.Errorf("PutTimeout(%d) = %v, want %v", size, got, want)
		}
	}
}
