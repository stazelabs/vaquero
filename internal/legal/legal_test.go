package legal_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/legal"
)

// Every module compiled into the binary has a notice with its licence. A
// dependency change without `make gen` fails here (and in CI's freshness
// check).
func TestThirdPartyCoversEveryLinkedModule(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{.Path}}{{end}}", "../../cmd/vaquero").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	have := map[string]legal.Component{}
	for _, c := range legal.ThirdParty() {
		have[c.Name] = c
	}
	for path := range strings.FieldsSeq(string(out)) {
		if path == "github.com/stazelabs/vaquero" {
			continue
		}
		c, ok := have[path]
		if !ok {
			t.Errorf("no notice for %s (run make gen)", path)
			continue
		}
		if len(c.License) < 100 {
			t.Errorf("%s: licence text missing", path)
		}
	}
	for _, name := range []string{"Go standard library", "htmx", "SortableJS", "Tailwind CSS"} {
		if c, ok := have[name]; !ok || len(c.License) < 100 {
			t.Errorf("no notice for %s", name)
		}
	}
}

// A module that ships several licence files (its own and those of code it
// includes, or a patent grant) lists all of them.
func TestNoticesIncludeEveryLicenceFile(t *testing.T) {
	have := map[string]string{}
	for _, c := range legal.ThirdParty() {
		have[c.Name] = c.License
	}
	for mod, file := range map[string]string{
		"modernc.org/sqlite": "SQLITE-LICENSE",
		"modernc.org/libc":   "LICENSE-3RD-PARTY.md",
		"golang.org/x/sys":   "PATENTS",
	} {
		if !strings.Contains(have[mod], "== "+file+" ==") {
			t.Errorf("%s: %s missing", mod, file)
		}
	}
}
