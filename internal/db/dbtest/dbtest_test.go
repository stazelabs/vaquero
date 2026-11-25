package dbtest

import (
	"os"
	"testing"
)

// VAQ-16: the template is kept in memory; its directory doesn't outlive it.
func TestTemplateLeavesNoDirectory(t *testing.T) {
	d := New(t)
	if err := d.W.Ping(); err != nil {
		t.Fatal(err)
	}
	if tplDir == "" {
		t.Fatal("the template was never built")
	}
	if _, err := os.Stat(tplDir); !os.IsNotExist(err) {
		t.Fatalf("template directory %s remains: %v", tplDir, err)
	}
}
