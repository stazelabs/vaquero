package lockfile_test

import (
	"errors"
	"os"
	"testing"

	"github.com/stazelabs/vaquero/internal/lockfile"
)

func TestAcquireRefusesSecondHolder(t *testing.T) {
	dir := t.TempDir()
	release, _, err := lockfile.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, held, err := lockfile.Acquire(dir)
	if !errors.Is(err, lockfile.ErrLocked) || held != os.Getpid() {
		t.Fatalf("second acquire: held=%d err=%v", held, err)
	}
	release()
	release2, _, err := lockfile.Acquire(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	release2()
}
