// Package static embeds the stylesheet, scripts and icon, and links them by
// a hash of their content so browsers never keep a stale copy.
package static

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sync"
)

// FS is the embedded tree: css/ (input.css and the generated tailwind.css),
// js/app.js, vendor/ (htmx, SortableJS), favicon.svg and logo.svg (the cowboy hat).
//
//go:embed css js vendor favicon.svg logo.svg
var FS embed.FS

var hashes = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := FS.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
	return out
})

// URL is the page link for an embedded file, such as "css/tailwind.css":
// its /static path with ?v= the first ten hex digits of its SHA-256.
func URL(path string) string {
	if h, ok := hashes()[path]; ok {
		return "/static/" + path + "?v=" + h
	}
	return "/static/" + path
}
