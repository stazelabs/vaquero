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
