package db_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tests outside internal/db must use dbtest.New, which migrates a template
// once per process, instead of migrating per test.
func TestNoMigrateOutsideDB(t *testing.T) {
	root, _ := filepath.Abs("../..")
	re := regexp.MustCompile(`\bappdb\.Migrate\(|\bdb\.Migrate\(`)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == ".claude") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(p, "_test.go") || strings.Contains(p, filepath.Join("internal", "db")+string(filepath.Separator)) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if re.Match(b) {
			t.Errorf("%s calls Migrate; use dbtest.New instead", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
