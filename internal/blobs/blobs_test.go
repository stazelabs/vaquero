package blobs_test

import (
	"strings"
	"testing"

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
