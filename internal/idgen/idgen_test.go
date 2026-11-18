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
