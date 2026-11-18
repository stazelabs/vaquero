package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestParseTicketKey(t *testing.T) {
	for in, want := range map[string]string{"VAQ-12": "VAQ-12", "vaq-12": "VAQ-12", " VAQ-012 ": "VAQ-12", "A1-3": "A1-3"} {
		k, ok := core.ParseTicketKey(in)
		if !ok || k.String() != want {
			t.Errorf("%q → %v %v", in, k, ok)
		}
	}
	for _, bad := range []string{"", "VAQ", "VAQ-", "VAQ-0", "VAQ-x", "-12", "VAQ-12-3", "nonsense", "1A-2", "VAQ-99999999999999999999"} {
		if _, ok := core.ParseTicketKey(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
