package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHolderReportsRunning(t *testing.T) {
	dir := t.TempDir()
	if _, running, err := Holder(dir); err != nil || running {
		t.Fatalf("free lock: running=%v err=%v", running, err)
	}
	release, _, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	// flock is per open file description, so a second open in this process
	// sees the lock as held, as another process would.
	pid, running, err := Holder(dir)
	if err != nil || !running || pid <= 0 {
		t.Fatalf("held lock: pid=%d running=%v err=%v", pid, running, err)
	}
	release()
	if _, running, _ := Holder(dir); running {
		t.Fatal("released lock still reads as running")
	}
}

func TestHolderMissingDir(t *testing.T) {
	if _, running, err := Holder(t.TempDir() + "/absent"); err != nil || running {
		t.Fatalf("missing dir: running=%v err=%v", running, err)
	}
}

func TestHolderLeavesThePidAlone(t *testing.T) {
	dir := t.TempDir()
	release, _, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	release() // the file stays, with this process's pid in it
	if err := os.WriteFile(filepath.Join(dir, "vaquero.lock"), []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Holder(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "vaquero.lock")); string(b) != "4242\n" {
		t.Fatalf("Holder rewrote the lock file: %q", b)
	}
}
