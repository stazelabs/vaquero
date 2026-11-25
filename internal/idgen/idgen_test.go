package idgen

import "testing"

func TestNewIsUniqueAndULIDShaped(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New()
		if len(id) != 26 {
			t.Fatalf("len(%q) = %d, want 26", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestValid(t *testing.T) {
	for s, want := range map[string]bool{
		New(): true, "01jx0000000000000000000000": true, "01JX0000000000000000000000": true,
		"": false, "x": false, "01JX000000000000000000000U": false, "../../etc/passwd0000000000": false, "01JX00000000000000000000000": false,
	} {
		if got := Valid(s); got != want {
			t.Errorf("Valid(%q) = %v", s, got)
		}
	}
}
