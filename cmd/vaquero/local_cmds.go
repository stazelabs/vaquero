package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// The commands here work on this machine's data directory without a
// server: stop and backup.
func init() {
	cliCommands["stop"] = cmdStop
	cliCommands["backup"] = cmdBackup
}

// stopWait is how long stop waits for the server to let go of its data
// directory: longer than serve's 30-second drain.
const stopWait = 40 * time.Second

// Hooks for tests: the clock, and the operating system brew services is
// looked up for.
var (
	localNow  = time.Now
	serviceOS = runtime.GOOS
)

// localConfig is the data directory and address `vaquero serve` would use.
func localConfig() (config.Config, error) {
	return config.FromEnv(os.Getenv)
}

// brewServiceFile is where brew services keeps Vaquero's launchd agent or
// systemd unit.
func brewServiceFile(home, goos string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "LaunchAgents", "homebrew.mxcl.vaquero.plist")
	}
	return filepath.Join(home, ".config", "systemd", "user", "homebrew.vaquero.service")
}

// brewService reports whether Vaquero runs under brew services, which
// restarts it whenever it stops.
func brewService() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(brewServiceFile(home, serviceOS))
	return err == nil
}

// message is an error that is a full sentence for the person running the
// command, printed as it is.
type message string

func (m message) Error() string { return string(m) }

// errBrewServices is stop's answer when the service manager owns the server.
var errBrewServices = message("Vaquero runs under brew services; run brew services stop vaquero.")

// stopLocal asks the server holding dataDir's lock to stop (SIGTERM, which
// drains like Ctrl-C) and waits until it lets go. stopped is false when
// nothing was running. A different server taking the lock meanwhile (a
// service manager's restart, or another command's) also ends the wait.
func stopLocal(ctx context.Context, dataDir string, wait time.Duration) (stopped bool, err error) {
	pid, running, err := lockfile.Holder(dataDir)
	if err != nil || !running {
		return false, err
	}
	if pid <= 0 {
		return false, fmt.Errorf("vaquero is running on %s, but its process id couldn't be read", dataDir)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return false, fmt.Errorf("couldn't stop vaquero (pid %d): %w", pid, err)
	}
	deadline := time.Now().Add(wait)
	for {
		if now, running, err := lockfile.Holder(dataDir); err != nil || !running || now != pid {
			return true, err
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("vaquero (pid %d) didn't stop within %v", pid, wait)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// cmdStop is `vaquero stop`: stop the local background server.
func cmdStop(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero stop")
	if _, err := c.parse(args, 0, 0); err != nil {
		return c.finish(err)
	}
	cfg, err := localConfig()
	if err != nil {
		return c.finish(err)
	}
	if brewService() {
		return c.finish(errBrewServices)
	}
	stopped, err := stopLocal(ctx, cfg.DataDir, stopWait)
	if err != nil {
		return c.finish(err)
	}
	if stopped {
		fmt.Fprintln(stdout, "Vaquero stopped.")
	} else {
		fmt.Fprintln(stdout, "Vaquero isn't running.")
	}
	return exitOK
}

const backupUsage = "vaquero backup list | vaquero backup restore NAME"

// cmdBackup is `vaquero backup list` and `vaquero backup restore NAME`.
func cmdBackup(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, backupUsage)
	pos, err := c.parse(args, 1, 2)
	if err != nil {
		return c.finish(err)
	}
	cfg, err := localConfig()
	if err != nil {
		return c.finish(err)
	}
	dir := filepath.Join(cfg.DataDir, "backups")
	switch {
	case pos[0] == "list" && len(pos) == 1:
		return c.finish(backupList(c, dir))
	case pos[0] == "restore" && len(pos) == 2:
		return c.finish(backupRestore(ctx, c, cfg, dir, pos[1]))
	}
	return c.finish(usageError{usage: backupUsage})
}

func backupList(c *cmd, dir string) error {
	all, err := appdb.ListBackups(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(c.stdout, "No backups yet.")
		return nil
	}
	var rows [][]string
	for _, b := range all {
		rows = append(rows, []string{b.Name, sizeWords(b.Size), timefmt.DisplayUTC(b.ModTime)})
	}
	c.table(rows)
	return nil
}

// sizeWords is a byte count for people: "812 KB", "3.4 MB".
func sizeWords(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", (n+1023)/1024)
	}
}

// backupRestore puts the backup called name in place of the database,
// after saving the current one as a backup, so a restore can be undone.
// The server must be stopped: it holds the database open.
func backupRestore(ctx context.Context, c *cmd, cfg config.Config, dir, name string) error {
	release, _, err := lockfile.Acquire(cfg.DataDir)
	if errors.Is(err, lockfile.ErrLocked) {
		if brewService() {
			return message("Stop vaquero first: brew services stop vaquero")
		}
		return message("Stop vaquero first: vaquero stop")
	}
	if errors.Is(err, os.ErrNotExist) {
		return message("No backup called " + name + ".")
	}
	if err != nil {
		return err
	}
	defer release()
	src, _, err := appdb.OpenBackup(dir, name)
	if errors.Is(err, appdb.ErrBackupNotFound) || errors.Is(err, appdb.ErrBadBackupName) || errors.Is(err, os.ErrNotExist) {
		return message("No backup called " + name + ".")
	}
	if err != nil {
		return err
	}
	defer src.Close()

	var saved string
	if _, err := os.Stat(cfg.DBPath); err == nil {
		cur, err := appdb.Open(cfg.DBPath)
		if err != nil {
			return err
		}
		info, err := appdb.Snapshot(ctx, cur, dir, localNow())
		_ = cur.Close()
		if err != nil {
			return fmt.Errorf("couldn't save the current database before restoring, so nothing changed: %w", err)
		}
		saved = info.Name
	}
	tmp := cfg.DBPath + ".restore"
	if err := copyFile(tmp, src); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// A stale WAL would be replayed onto the restored file.
	for _, ext := range []string{"-wal", "-shm"} {
		if err := os.Remove(cfg.DBPath + ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, cfg.DBPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if saved == "" {
		fmt.Fprintf(c.stdout, "Restored %s.\n", name)
	} else {
		fmt.Fprintf(c.stdout, "Restored %s. The database before it is saved as %s.\n", name, saved)
	}
	return nil
}

// copyFile writes src to path (0600) and syncs it.
func copyFile(path string, src io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
