package units

import "testing"

func TestSizeWords(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 KB", 1: "1 KB", 1024: "1 KB", 1025: "2 KB", 812 * 1024: "812 KB",
		1 << 20: "1.0 MB", 3<<20 + 400<<10: "3.4 MB", 10 << 20: "10.0 MB", 2 << 30: "2.0 GB",
	} {
		if got := SizeWords(n); got != want {
			t.Errorf("SizeWords(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestClipCutsCharactersNotBytes(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 2, "ab"},
		{"École", 1, "É"},
		{"éééé", 3, "ééé"},
		{"🐄🐄🐄", 2, "🐄🐄"},
		{"", 3, ""},
	} {
		if got := Clip(c.in, c.n); got != c.want {
			t.Errorf("Clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
