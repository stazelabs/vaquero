// Command notices writes internal/legal/third_party.json: every module
// linked into cmd/vaquero (for linux/amd64 and darwin/arm64, the release
// targets), the Go standard library, and the browser files bundled under
// internal/web/static, each with its licence text. No Go version is
// recorded, so the output doesn't depend on the local toolchain. Run from the repository
// root (make gen does).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type component struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	URL     string `json:"url,omitempty"`
	License string `json:"license"`
}

// bundled are browser files copied into the binary; their licence texts
// live in internal/legal/vendor.
//
// The Go standard library is listed here too: its licence is kept in the
// vendor directory (some Go installs omit $GOROOT/LICENSE), which also keeps
// the output the same on every machine.
var bundled = []struct{ name, version, url, file string }{
	{"Go standard library", "", "https://go.dev", "go.txt"},
	{"htmx", "2.0.4", "https://htmx.org", "htmx.txt"},
	{"Lucide", "1.48.0", "https://lucide.dev", "lucide.txt"},
	{"SortableJS", "1.15.6", "https://github.com/SortableJS/Sortable", "sortablejs.txt"},
	{"Tailwind CSS", "4.3.5", "https://tailwindcss.com", "tailwindcss.txt"},
}

const self = "github.com/stazelabs/vaquero"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run() error {
	mods := map[string]component{}
	for _, target := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}} {
		if err := addModules(mods, target[0], target[1]); err != nil {
			return err
		}
	}
	for _, b := range bundled {
		text, err := os.ReadFile(filepath.Join("internal", "legal", "vendor", b.file))
		if err != nil {
			return err
		}
		mods[b.name] = component{Name: b.name, Version: b.version, URL: b.url, License: string(text)}
	}
	out := make([]component, 0, len(mods))
	for _, c := range mods {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b component) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("internal", "legal", "third_party.json"), append(b, '\n'), 0o644)
}

type pkg struct {
	Standard bool
	Module   *struct {
		Path, Version, Dir string
		Replace            *struct{ Path, Version, Dir string }
	}
}

// addModules adds every non-standard module linked into cmd/vaquero for
// one target.
func addModules(mods map[string]component, goos, goarch string) error {
	cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/vaquero")
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("go list (%s/%s): %w", goos, goarch, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return err
		}
		if p.Standard || p.Module == nil || p.Module.Path == self {
			continue
		}
		m := p.Module
		if _, done := mods[m.Path]; done {
			continue
		}
		dir := m.Dir
		if m.Replace != nil && m.Replace.Dir != "" {
			dir = m.Replace.Dir
		}
		text, err := licenseIn(dir)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Path, err)
		}
		mods[m.Path] = component{Name: m.Path, Version: m.Version, URL: "https://pkg.go.dev/" + m.Path, License: text}
	}
	return nil
}

// licenseIn reads every licence-like file at the top of a module (its own
// licence, and any notices, patent grants or licences of code it includes),
// each under an "== name ==" header, in name order.
func licenseIn(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(n, "licen") || strings.HasPrefix(n, "copying") || strings.HasPrefix(n, "notice") ||
			n == "patents" || n == "unlicense" || strings.HasSuffix(n, "-license") || strings.HasSuffix(n, "-licence") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no licence file in %s", dir)
	}
	slices.Sort(names)
	var b strings.Builder
	for i, n := range names {
		text, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "== %s ==\n\n%s", n, strings.ReplaceAll(string(text), "\r\n", "\n"))
	}
	return b.String(), nil
}
