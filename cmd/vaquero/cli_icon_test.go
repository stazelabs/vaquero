package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLIProjectIcon(t *testing.T) {
	e := cliEnv(t)
	if out := mustCLI(t, "project", "icon", "vaq", "--glyph", "rocket"); strings.TrimSpace(out) != "VAQ icon: glyph rocket" {
		t.Fatalf("%q", out)
	}
	png := writeFile(t, "logo.png", tinyPNG)
	var p api.Project
	if err := json.Unmarshal([]byte(mustCLI(t, "project", "icon", "VAQ", "--image", png, "--json")), &p); err != nil ||
		p.Icon == nil || p.Icon.Kind != "image" || p.Icon.URL != "/p/VAQ/icon" {
		t.Fatalf("%v %+v", err, p)
	}
	vaq, err := e.Deps.R.GetProjectByKey(t.Context(), "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := e.Deps.R.GetProjectIcon(t.Context(), vaq.ID); got.Type != "image/png" {
		t.Fatalf("stored %q", got.Type)
	}
	if out := mustCLI(t, "project", "icon", "VAQ", "--monogram"); strings.TrimSpace(out) != "VAQ icon: monogram VA" {
		t.Fatalf("%q", out)
	}

	// Refusals: the server's words, exit 1.
	if code, _, errOut := cli(t, "project", "icon", "VAQ", "--glyph", "unicorn"); code != 1 || !strings.Contains(errOut, "Choose a glyph from the list.") {
		t.Fatalf("%d %q", code, errOut)
	}
	txt := writeFile(t, "fake.png", []byte("not an image at all"))
	if code, _, errOut := cli(t, "project", "icon", "VAQ", "--image", txt); code != 1 || !strings.Contains(errOut, "Use a PNG, JPEG or WebP image.") {
		t.Fatalf("%d %q", code, errOut)
	}
	big := writeFile(t, "big.png", append(append([]byte{}, tinyPNG...), make([]byte, 300<<10)...))
	if code, _, errOut := cli(t, "project", "icon", "VAQ", "--image", big); code != 1 || !strings.Contains(errOut, "Use an image of 256 KB or less.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "icon", "VAQ", "--image", "/no/such/file.png"); code != 1 || errOut == "" {
		t.Fatalf("%d %q", code, errOut)
	}
	// Exactly one of the three.
	for _, args := range [][]string{
		{"project", "icon", "VAQ"},
		{"project", "icon", "VAQ", "--monogram", "--glyph", "rocket"},
		{"project", "edit", "VAQ", "--monogram"},
	} {
		if code, _, _ := cli(t, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}
