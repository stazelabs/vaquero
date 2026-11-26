package gitread_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runsGit is any exec.Command or exec.CommandContext whose program is the
// literal "git", or a lookup of git on PATH.
var runsGit = regexp.MustCompile(`exec\.Command(Context)?\(\s*(ctx\s*,\s*)?"git"|exec\.LookPath\(\s*"git"\s*\)`)

// Only internal/gitread runs git: everything else asks it.
func TestOnlyGitreadRunsGit(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(root, "internal", "gitread")
	found := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "tmp", "bin", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !runsGit.Match(b) {
			return nil
		}
		if path == own || strings.HasPrefix(path, own+string(filepath.Separator)) {
			found++
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		t.Errorf("%s runs git; only internal/gitread may", rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("the pattern finds no git command even in gitread")
	}
}
