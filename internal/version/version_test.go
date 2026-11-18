package version

import "testing"

func TestLine(t *testing.T) {
	Version, Commit, BuildDate = "1.2.3", "abc1234", ""
	if got := Line(); got != "vaquero 1.2.3 (abc1234)" {
		t.Fatalf("%q", got)
	}
	BuildDate = "2026-11-18T00:00:00Z"
	if got := Line(); got != "vaquero 1.2.3 (abc1234, 2026-11-18T00:00:00Z)" {
		t.Fatalf("%q", got)
	}
}
