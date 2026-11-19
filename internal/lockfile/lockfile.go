//go:build unix

// Package lockfile keeps two vaquero servers from sharing a data directory:
// serve holds an exclusive flock on {dir}/vaquero.lock for as long as it
// runs. The kernel drops the lock when the process exits, however it exits.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("locked")

// Acquire takes the lock on dir and writes this process's pid into the
// file. When another process holds it, err is ErrLocked and held is that
// process's pid (0 if unreadable). release unlocks and closes.
func Acquire(dir string) (release func(), held int, err error) {
	path := filepath.Join(dir, "vaquero.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("open lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		b, _ := os.ReadFile(path)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, pid, ErrLocked
		}
		return nil, 0, fmt.Errorf("lock: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, 0, nil
}

// Holder reports whether a process holds the lock on dir, and its pid. A
// missing directory or lock file has no holder. It only probes: the file
// is never created or written, so the holder's pid stays in it.
func Holder(dir string) (pid int, running bool, err error) {
	path := filepath.Join(dir, "vaquero.lock")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("open lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			b, _ := os.ReadFile(path)
			pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
			return pid, true, nil
		}
		return 0, false, fmt.Errorf("lock: %w", err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return 0, false, nil
}
