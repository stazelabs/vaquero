package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tmplPath = "../../packaging/homebrew/vaquero.rb.tmpl"

func readTemplate(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func readChecksums(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The golden file is the formula goreleaser pushed for v0.1.2, with only
// its header comment changed: the template reproduces the service block,
// the install and the test, for both architectures on macOS and Linux.
func TestRenderMatchesGolden(t *testing.T) {
	got, err := render(readTemplate(t), "0.1.2", strings.NewReader(readChecksums(t)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "vaquero.rb.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("rendered formula differs from the golden file:\n%s", got)
	}
	for _, s := range []string{
		"service do\n    run [opt_bin/\"vaquero\", \"serve\"]\n    keep_alive true",
		"vaquero_0.1.2_darwin_amd64.tar.gz", "vaquero_0.1.2_darwin_arm64.tar.gz",
		"vaquero_0.1.2_linux_amd64.tar.gz", "vaquero_0.1.2_linux_arm64.tar.gz",
	} {
		if !strings.Contains(string(got), s) {
			t.Errorf("formula lacks %q", s)
		}
	}
	if strings.Contains(strings.ToLower(string(got)), "token") {
		t.Error("formula mentions a token")
	}
}

// A leading v is dropped, so the workflow can pass the tag as it is.
func TestRenderAcceptsTheTag(t *testing.T) {
	a, err := render(readTemplate(t), "v0.1.2", strings.NewReader(readChecksums(t)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := render(readTemplate(t), "0.1.2", strings.NewReader(readChecksums(t)))
	if string(a) != string(b) {
		t.Fatal("v0.1.2 and 0.1.2 render differently")
	}
}

// Other files in checksums.txt are ignored; each archive's line counts.
func TestRenderIgnoresOtherFiles(t *testing.T) {
	sums := "0000000000000000000000000000000000000000000000000000000000000000  vaquero_0.1.2_windows_amd64.zip\n" + readChecksums(t)
	if _, err := render(readTemplate(t), "0.1.2", strings.NewReader(sums)); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRefusals(t *testing.T) {
	sums := readChecksums(t)
	drop := func(name string) string {
		var keep []string
		for line := range strings.SplitSeq(sums, "\n") {
			if !strings.Contains(line, name) {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n")
	}
	for name, c := range map[string]struct{ version, sums, want string }{
		"missing archive":   {"0.1.2", drop("linux_arm64"), "vaquero_0.1.2_linux_arm64.tar.gz"},
		"other version":     {"0.1.3", sums, "vaquero_0.1.3_darwin_amd64.tar.gz"},
		"bad version":       {"0.1", sums, "version"},
		"space in version":  {"0.1.2 x", sums, "version"},
		"short checksum":    {"0.1.2", strings.Replace(sums, "eb61e314", "", 1), "checksum"},
		"upper case sum":    {"0.1.2", strings.Replace(sums, "eb61e314", "EB61E314", 1), "checksum"},
		"duplicate archive": {"0.1.2", sums + "01acd77f38b2fd3a8d635a422f59a048c4e9f57ec6c958c3bfad25b5b4654522  vaquero_0.1.2_darwin_arm64.tar.gz\n", "twice"},
		"bad line":          {"0.1.2", sums + "nonsense\n", "line 5"},
	} {
		_, err := render(readTemplate(t), c.version, strings.NewReader(c.sums))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error naming %q", name, err, c.want)
		}
	}
}

// A snapshot's version renders, for a local check of goreleaser's dist.
func TestRenderSnapshot(t *testing.T) {
	sums := strings.ReplaceAll(readChecksums(t), "_0.1.2_", "_0.1.3-SNAPSHOT-abc1234_")
	got, err := render(readTemplate(t), "0.1.3-SNAPSHOT-abc1234", strings.NewReader(sums))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "download/v0.1.3-SNAPSHOT-abc1234/vaquero_0.1.3-SNAPSHOT-abc1234_linux_arm64.tar.gz") {
		t.Fatalf("snapshot formula:\n%s", got)
	}
}

// run writes the formula to -o, and refuses to run without -version.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "vaquero.rb")
	if err := run([]string{"-version", "0.1.2", "-checksums", "testdata/checksums.txt", "-template", tmplPath, "-o", out}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	want, _ := os.ReadFile("testdata/vaquero.rb.golden")
	if string(got) != string(want) {
		t.Fatal("-o file differs from the golden file")
	}
	if err := run([]string{"-checksums", "testdata/checksums.txt"}); err == nil {
		t.Fatal("ran without -version")
	}
}
