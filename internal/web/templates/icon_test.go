package templates

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/icons"
)

// Review Focus 1: every icon a template or a Go table names is in ui.json.
var iconCall = regexp.MustCompile(`Icon(?:Lg|Class)?\("([a-z0-9-]+)"|(?:icon|Icon)\s*[:=]\s*"([a-z0-9-]+)"`)

func TestEveryIconNameKnown(t *testing.T) {
	files, _ := filepath.Glob("*.templ")
	goFiles, _ := filepath.Glob("*.go")
	files = append(files, goFiles...)
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_templ.go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range iconCall.FindAllStringSubmatch(string(b), -1) {
			name := m[1] + m[2]
			seen++
			if _, ok := icons.UI(name); !ok {
				t.Errorf("%s: icon %q isn't in ui.json", f, name)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no icon uses found")
	}
}

func TestIconMarkup(t *testing.T) {
	var b bytes.Buffer
	if err := Icon("search").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-hidden="true"`, `stroke="currentColor"`, "h-4 w-4", `data-icon="search"`, "<circle", "<path"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lacks %q: %s", want, b.String())
		}
	}
	b.Reset()
	IconLg("search").Render(context.Background(), &b)
	if !strings.Contains(b.String(), "h-5 w-5") {
		t.Fatal(b.String())
	}
	b.Reset()
	Icon("no-such-icon").Render(context.Background(), &b)
	if b.Len() != 0 {
		t.Fatalf("unknown icon drew %q", b.String())
	}
}

func TestNoAdHocSVG(t *testing.T) {
	files, _ := filepath.Glob("*.templ")
	for _, f := range files {
		if f == "icon.templ" {
			continue
		}
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte("<svg")) {
			t.Errorf("%s draws its own <svg>; use Icon", f)
		}
	}
}
