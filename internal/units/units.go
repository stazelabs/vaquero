// Package units words quantities for people and cuts text by characters.
package units

import "fmt"

// SizeWords is a byte count for people: "812 KB", "3.4 MB".
func SizeWords(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", (n+1023)/1024)
	}
}

// Clip cuts s to at most n characters (runes), never inside one.
func Clip(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
