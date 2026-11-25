package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

// hold takes the flock on dir's lock file through its own descriptor, as
// another process would, with content in it.
func hold(t *testing.T, dir, content string) (path string, release func()) {
	t.Helper()
	path = filepath.Join(dir, "vaquero.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
}

// Holder takes the lock for an instant to probe it; a server starting at
// that instant isn't turned away.
func TestAcquireRidesOutAProbe(t *testing.T) {
	dir := t.TempDir()
	_, release := hold(t, dir, "")
	go func() { time.Sleep(20 * time.Millisecond); release() }()
	rel, _, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire during a brief hold: %v", err)
	}
	rel()
}

func TestAcquireStillReportsARealHolder(t *testing.T) {
	dir := t.TempDir()
	_, release := hold(t, dir, "4242\n")
	defer release()
	start := time.Now()
	_, pid, err := Acquire(dir)
	if !errors.Is(err, ErrLocked) || pid != 4242 {
		t.Fatalf("pid %d, err %v", pid, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v to say so", time.Since(start))
	}
}

// A server writes its pid just after it takes the lock; until then the
// file holds the last holder's pid, or nothing. Holder waits for it to
// settle rather than naming a stale process.
func TestHolderIgnoresAPidThatIsStillBeingWritten(t *testing.T) {
	for name, before := range map[string]string{"stale": "99999\n", "empty": ""} {
		dir := t.TempDir()
		path, release := hold(t, dir, before)
		go func() { time.Sleep(5 * time.Millisecond); _ = os.WriteFile(path, []byte("4242\n"), 0o600) }()
		pid, running, err := Holder(dir)
		release()
		if err != nil || !running || pid != 4242 {
			t.Errorf("%s: pid %d running %v err %v", name, pid, running, err)
		}
	}
}

// A maintenance holder (restore, import) says so, so stop can leave it be.
func TestHolderRoleNamesMaintenance(t *testing.T) {
	dir := t.TempDir()
	release, _, err := AcquireRole(dir, RoleMaintenance)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	pid, role, running, err := HolderRole(dir)
	if err != nil || !running || pid != os.Getpid() || role != RoleMaintenance {
		t.Fatalf("pid %d role %q running %v err %v", pid, role, running, err)
	}
	if p, _, _ := Holder(dir); p != os.Getpid() {
		t.Fatalf("Holder pid %d", p)
	}
}
