package legal_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/legal"
)

func noticeVersion(t *testing.T, name string) string {
	t.Helper()
	for _, c := range legal.ThirdParty() {
		if c.Name == name {
			return c.Version
		}
	}
	t.Fatalf("no notice for %s", name)
	return ""
}

func fileHead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The versions scripts/notices records for the bundled browser files and
// icons are the versions of the files in the tree. A file updated without
// its notice (or the other way round) fails here.
func TestBundledVersionsMatchTheFiles(t *testing.T) {
	const static = "../web/static/"
	cases := []struct {
		name, file string
		re         *regexp.Regexp
	}{
		{"htmx", static + "vendor/htmx.min.js", regexp.MustCompile(`version:"([^"]+)"`)},
		{"SortableJS", static + "vendor/sortable.min.js", regexp.MustCompile(`^/\*! Sortable ([0-9][^ ]*) `)},
		{"Tailwind CSS", static + "css/tailwind.css", regexp.MustCompile(`^/\*! tailwindcss v([0-9][^ ]*) `)},
		{"Lucide", "../web/icons/ui_gen.go", regexp.MustCompile(`lucide-static@([0-9][^ ]*) `)},
	}
	for _, c := range cases {
		m := c.re.FindStringSubmatch(fileHead(t, c.file))
		if m == nil {
			t.Errorf("%s: no version found in %s", c.name, c.file)
			continue
		}
		if got := noticeVersion(t, c.name); got != m[1] {
			t.Errorf("%s: the notice says %s, %s says %s (update scripts/notices and run make gen)", c.name, got, c.file, m[1])
		}
	}
}

// package.json pins the build-time packages exactly, so a fresh install
// can't pick a different Tailwind or Lucide than the notices record.
func TestPackageJSONPinsAreExactAndMatchTheNotices(t *testing.T) {
	var pkg struct {
		Dev map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(fileHead(t, "../../package.json")), &pkg); err != nil {
		t.Fatal(err)
	}
	for name, v := range pkg.Dev {
		if strings.ContainsAny(v, "^~<>*|x ") {
			t.Errorf("%s is %q: pin an exact version", name, v)
		}
	}
	for dep, notice := range map[string]string{
		"tailwindcss":      "Tailwind CSS",
		"@tailwindcss/cli": "Tailwind CSS",
		"lucide-static":    "Lucide",
	} {
		if got, want := pkg.Dev[dep], noticeVersion(t, notice); got != want {
			t.Errorf("package.json %s is %q, the %s notice says %q", dep, got, notice, want)
		}
	}
}

// Notice links go to pkg.go.dev: a module path ending in /v2 and up has no
// page at its own address.
func TestModuleNoticeLinksWork(t *testing.T) {
	for _, c := range legal.ThirdParty() {
		if !strings.Contains(c.Name, ".") || strings.HasPrefix(c.URL, "https://pkg.go.dev/") {
			continue
		}
		if c.Name == "Go standard library" {
			continue
		}
		t.Errorf("%s links to %s, want a pkg.go.dev link", c.Name, c.URL)
	}
}
