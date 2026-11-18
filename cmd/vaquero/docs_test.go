package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/help"
)

func TestDocsCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"docs"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "cli") || !strings.Contains(out.String(), "The command line") {
		t.Fatalf("list: %d %q", code, out.String())
	}
	out.Reset()
	a, _ := help.Get("cli")
	if code := run([]string{"docs", "cli"}, &out, &errOut); code != 0 || out.String() != a.Markdown {
		t.Fatalf("cli: %d", code)
	}
	errOut.Reset()
	if code := run([]string{"docs", "nope"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "vaquero docs") {
		t.Fatalf("unknown: %d %q", code, errOut.String())
	}
}

// Review Focus 3: every command is in the CLI article.
func TestCLICommandsDocumented(t *testing.T) {
	a, _ := help.Get("cli")
	names := []string{"serve", "version"}
	for n := range cliCommands {
		names = append(names, n)
	}
	for _, n := range names {
		if !strings.Contains(a.Markdown, "`vaquero "+n) {
			t.Errorf("vaquero %s isn't in the cli article", n)
		}
	}
}
