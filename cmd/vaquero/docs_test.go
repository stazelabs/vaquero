package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
		if !strings.Contains(a.Markdown, "`vaquero "+n+" ") && !strings.Contains(a.Markdown, "`vaquero "+n+"`") {
			t.Errorf("vaquero %s isn't in the cli article", n)
		}
	}
}

// sourceOf reads a non-test source file of this package.
func sourceOf(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every subcommand of the commands that have them is in that command's row
// of the cli article, so a verb added in code without a line fails here.
func TestCLISubcommandsDocumented(t *testing.T) {
	a, _ := help.Get("cli")
	verb := regexp.MustCompile(`case "([a-z]+)":|pos\[0\] == "([a-z]+)"`)
	for _, c := range []struct{ file, fn, cmd string }{
		{"cli_cmds.go", "cmdProject", "project"},
		{"cli_cmds.go", "cmdTicket", "ticket"},
		{"cli_cmds.go", "cmdToken", "token"},
		{"cli_cmds.go", "cmdProfile", "profile"},
		{"cli_statuses.go", "cmdStatus", "status"},
	} {
		src := sourceOf(t, c.file)
		i := strings.Index(src, "\nfunc "+c.fn+"(")
		if i < 0 {
			t.Fatalf("%s: no func %s", c.file, c.fn)
		}
		body := src[i+1:]
		if j := strings.Index(body[1:], "\nfunc "); j >= 0 {
			body = body[:j+1]
		}
		var rows []string
		for l := range strings.SplitSeq(a.Markdown, "\n") {
			if strings.HasPrefix(l, "| `vaquero "+c.cmd+" ") {
				rows = append(rows, l)
			}
		}
		seen := 0
		for _, m := range verb.FindAllStringSubmatch(body, -1) {
			v := m[1] + m[2]
			seen++
			ok := false
			for _, r := range rows {
				ok = ok || strings.Contains(r, "`vaquero "+c.cmd+" "+v) || strings.Contains(r, "`"+v+"`") || strings.Contains(r, "`"+v+" ")
			}
			if !ok {
				t.Errorf("vaquero %s %s isn't in the cli article", c.cmd, v)
			}
		}
		if seen == 0 {
			t.Errorf("%s: found no subcommands in %s; the scan needs updating", c.cmd, c.fn)
		}
	}
	// backup: both verbs, wherever they are dispatched.
	for _, v := range []string{"list", "restore"} {
		if !strings.Contains(a.Markdown, "`vaquero backup "+v) {
			t.Errorf("vaquero backup %s isn't in the cli article", v)
		}
	}
}

// Every flag a command registers is in the cli article.
func TestCLIFlagsDocumented(t *testing.T) {
	a, _ := help.Get("cli")
	flagRe := regexp.MustCompile(`\.fs\.(?:String|Bool|Int|Int64|Duration)\("([a-z-]+)"|\.fs\.(?:String|Bool|Int|Int64|Duration|)Var\(&?[A-Za-z.]+, "([a-z-]+)"`)
	files, _ := filepath.Glob("*.go")
	seen := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		for _, m := range flagRe.FindAllStringSubmatch(sourceOf(t, f), -1) {
			name := m[1] + m[2]
			seen++
			// A one-letter flag is written with one dash (-n, -f).
			spelled := "--" + name
			if len(name) == 1 {
				spelled = "-" + name
			}
			if !regexp.MustCompile(regexp.QuoteMeta(spelled) + `\b`).MatchString(a.Markdown) {
				t.Errorf("%s: %s isn't in the cli article", f, spelled)
			}
		}
	}
	if seen < 15 {
		t.Fatalf("found %d flags; the scan needs updating", seen)
	}
}
