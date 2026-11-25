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
	"time"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("locked")

// RoleMaintenance marks a holder that replaces the database (restore,
// import): it must not be stopped like a server. A server has no role.
const RoleMaintenance = "maintenance"

// Timing: Holder probes by taking the lock for an instant, so Acquire
// retries a refusal for acquireRetry before believing another holder; and a
// holder writes its pid just after it takes the lock, so Holder reads it
// until two reads, pidSettle apart, agree.
const (
	acquireRetry = 60 * time.Millisecond
	acquirePoll  = 5 * time.Millisecond
	pidSettle    = 20 * time.Millisecond
	pidWait      = 200 * time.Millisecond
)

// Acquire takes the lock on dir and writes this process's pid into the
// file. When another process holds it, err is ErrLocked and held is that
// process's pid (0 if unreadable). release unlocks and closes.
func Acquire(dir string) (release func(), held int, err error) {
	return AcquireRole(dir, "")
}

// AcquireRole is Acquire for a holder with a role (RoleMaintenance), which
// is written beside the pid.
func AcquireRole(dir, role string) (release func(), held int, err error) {
	path := filepath.Join(dir, "vaquero.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("open lock: %w", err)
	}
	deadline := time.Now().Add(acquireRetry)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			break
		}
		time.Sleep(acquirePoll)
	}
	if err != nil {
		pid, _ := readHolder(path)
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, pid, ErrLocked
		}
		return nil, 0, fmt.Errorf("lock: %w", err)
	}
	content := strconv.Itoa(os.Getpid())
	if role != "" {
		content += " " + role
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(content+"\n"), 0)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, 0, nil
}

// readHolder reads the pid, and role if any, from the lock file.
func readHolder(path string) (pid int, role string) {
	b, _ := os.ReadFile(path)
	fields := strings.Fields(string(b))
	if len(fields) > 0 {
		pid, _ = strconv.Atoi(fields[0])
	}
	if len(fields) > 1 {
		role = fields[1]
	}
	return pid, role
}

// settledHolder reads the holder's pid and role once two reads a moment
// apart agree and name a pid: the holder writes them just after taking the
// lock, so a read right then can be empty or the last holder's. Past
// pidWait it returns what it has.
func settledHolder(path string) (int, string) {
	deadline := time.Now().Add(pidWait)
	pid, role := readHolder(path)
	for {
		time.Sleep(pidSettle)
		p2, r2 := readHolder(path)
		if p2 == pid && r2 == role && pid > 0 || time.Now().After(deadline) {
			return p2, r2
		}
		pid, role = p2, r2
	}
}

// Holder reports whether a process holds the lock on dir, and its pid. A
// missing directory or lock file has no holder. It only probes: the file
// is never created or written, so the holder's pid stays in it.
func Holder(dir string) (pid int, running bool, err error) {
	pid, _, running, err = HolderRole(dir)
	return pid, running, err
}

// HolderRole is Holder that also returns the holder's role ("" for a
// server, RoleMaintenance for a restore or import).
func HolderRole(dir string) (pid int, role string, running bool, err error) {
	path := filepath.Join(dir, "vaquero.lock")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("open lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			pid, role = settledHolder(path)
			return pid, role, true, nil
		}
		return 0, "", false, fmt.Errorf("lock: %w", err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return 0, "", false, nil
}
