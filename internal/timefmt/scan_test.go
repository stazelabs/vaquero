package timefmt_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// formatCall finds a time layout passed to Format. An instant reaches a
// person only through templates.When and friends or timefmt.Display*, so a
// layout of one's own anywhere else is how a UTC-only or wrongly zoned time
// gets back in.
var formatCall = regexp.MustCompile(`\.Format\(`)

// allowed lists the files that format with a layout of their own, and why.
// A calendar date (time.DateOnly) is allowed anywhere: it names a day, not
// an instant.
var allowed = map[string]string{
	"internal/timefmt/timefmt.go":       "the one place",
	"internal/db/backup.go":             "a file name",
	"internal/blobs/blobtest/s3fake.go": "test fake: S3 wire-protocol timestamp layouts",
}

func TestInstantsFormatOnlyThroughTimefmt(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "gen", "bin", "tmp", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if (!strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, ".templ")) ||
			strings.HasSuffix(rel, "_templ.go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if _, ok := allowed[rel]; ok {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !formatCall.MatchString(line) || strings.Contains(line, "time.DateOnly") ||
				strings.Contains(line, "timefmt.Format(") {
				continue
			}
			bad = append(bad, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Errorf("format instants through timefmt.Format (storage), timefmt.Display* or templates.When*, "+
			"or add the file to the allowlist with a reason:\n%s", strings.Join(bad, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}
