package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Handlers change the tracker only through Deps.Core: no handler opens a
// transaction, builds writer queries or executes SQL itself. misc.go's
// /healthz ping is the one use of the writer pool.
func TestHandlersNeverWriteDirectly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("%v %v", files, err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, bad := range []string{"gen.New(", ".Exec(", "BeginTx(", "d.DB."} {
			if strings.Contains(src, bad) && !(f == "misc.go" && bad == "d.DB.") {
				t.Errorf("%s uses %s; change the tracker through d.Core", f, bad)
			}
		}
	}
}
