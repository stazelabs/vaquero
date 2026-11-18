package apitoken_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/apitoken"
)

func TestMintParseHash(t *testing.T) {
	raw, err := apitoken.Mint()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "vq_") || len(raw) != 3+43 || !apitoken.Valid(raw) {
		t.Fatalf("%q", raw)
	}
	other, _ := apitoken.Mint()
	if other == raw {
		t.Fatal("two mints alike")
	}
	if got, ok := apitoken.Parse("bearer " + raw); !ok || got != raw {
		t.Fatalf("parse lowercase scheme: %q %v", got, ok)
	}
	if got, ok := apitoken.Parse("Bearer  " + raw + " "); !ok || got != raw {
		t.Fatalf("parse spaces: %q %v", got, ok)
	}
	for _, bad := range []string{"", raw, "Basic " + raw, "Bearer", "Bearer vq_short", "Bearer hc_" + raw[3:], "Bearer " + raw + "x", "Bearer " + raw[:45] + "!"} {
		if _, ok := apitoken.Parse(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if apitoken.Prefix(raw) != raw[:9] {
		t.Fatal(apitoken.Prefix(raw))
	}
	h1, h2 := apitoken.Hash(raw), apitoken.Hash(raw[:len(raw)-0])
	if h1 != h2 || h1 == apitoken.Hash(other) || strings.Contains(h1, raw[3:]) {
		t.Fatal("hash")
	}
}
