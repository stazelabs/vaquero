package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
)

// The commands here work on this machine's data directory without a
// server: stop and backup (and import, in cli_import.go).
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

// localConfig is the data directory and address `vaquero serve` would use
// (config.Local: VQ_MODE, VQ_ADDR, VQ_DATA_DIR), for stop and backup; a bad
// server-only setting doesn't stop them.
func localConfig() (config.Config, error) {
	return config.Local(os.Getenv)
}

// brewServiceFile is where brew services keeps Vaquero's launchd agent or
// systemd unit.
func brewServiceFile(home, goos string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "LaunchAgents", "homebrew.mxcl.vaquero.plist")
	}
	return filepath.Join(home, ".config", "systemd", "user", "homebrew.vaquero.service")
}

// brewService reports whether the server on dataDir is the one brew
// services runs, which restarts it whenever it stops.
func brewService(dataDir string) bool {
	return brewManaged(serviceOS, dataDir, os.Getenv)
}

// brewManaged reports whether brew services runs Vaquero on dataDir: the
// service file exists, and dataDir is the default data directory, the one
// the formula's service uses (it sets no VQ_DATA_DIR). An instance with a
// data directory of its own is never the service's, so stop and upgrades
// treat it as any other.
func brewManaged(goos, dataDir string, get func(string) string) bool {
	home := get("HOME")
	if home == "" {
		return false
	}
	if _, err := os.Stat(brewServiceFile(home, goos)); err != nil {
		return false
	}
	def, err := config.DefaultDataDir(goos, get)
	if err != nil {
		return false
	}
	return sameDir(dataDir, def)
}

// sameDir reports whether a and b name the same directory: the same file
// when both exist (a symlink is followed), else the same clean absolute
// path.
func sameDir(a, b string) bool {
	if sa, err := os.Stat(a); err == nil {
		if sb, err := os.Stat(b); err == nil {
			return os.SameFile(sa, sb)
		}
	}
	aa, err1 := filepath.Abs(a)
	ab, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == ab
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
//
// expect, when not 0, is the pid of the server the caller meant to stop (the
// one that answered its probe): if another holds the lock by now, it was
// already replaced and nothing is stopped. A restore, import or compaction
// holds the lock too, and is never signalled.
func stopLocal(ctx context.Context, dataDir string, wait time.Duration, expect int) (stopped bool, err error) {
	pid, role, running, err := lockfile.HolderRole(dataDir)
	if err != nil || !running {
		return false, err
	}
	if role == lockfile.RoleMaintenance {
		return false, message("A restore, import or compaction is replacing the database in " + dataDir + "; wait for it to finish.")
	}
	if expect != 0 && pid != expect {
		return false, nil
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
	if brewService(cfg.DataDir) {
		return c.finish(errBrewServices)
	}
	stopped, err := stopLocal(ctx, cfg.DataDir, stopWait, 0)
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

const backupUsage = "vaquero backup list | vaquero backup restore NAME | vaquero backup compact"

// cmdBackup is `vaquero backup list`, `vaquero backup restore NAME` and
// `vaquero backup compact`.
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
	case pos[0] == "compact" && len(pos) == 1:
		return c.finish(backupCompact(ctx, c, cfg, dir))
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
		rows = append(rows, []string{b.Name, units.SizeWords(b.Size), timefmt.DisplayUTC(b.ModTime)})
	}
	c.table(rows)
	return nil
}

// lockStopped takes the data directory's lock, which a running server
// holds: the commands that replace the database need it stopped.
func lockStopped(dataDir string) (release func(), err error) {
	release, _, err = lockfile.AcquireRole(dataDir, lockfile.RoleMaintenance)
	if errors.Is(err, lockfile.ErrLocked) {
		if brewService(dataDir) {
			return nil, message("Stop vaquero first: brew services stop vaquero")
		}
		return nil, message("Stop vaquero first: vaquero stop")
	}
	return release, err
}

// backupRestore puts the backup called name in place of the database,
// after saving the current one as a backup, so a restore can be undone.
// The server must be stopped: it holds the database open.
func backupRestore(ctx context.Context, c *cmd, cfg config.Config, dir, name string) error {
	release, err := lockStopped(cfg.DataDir)
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

	tmp := cfg.DBPath + ".restore"
	if err := copyFile(tmp, src); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// A snapshot is a VACUUM INTO copy, which may renumber the rowids the
	// search indexes are keyed on.
	if err := appdb.RebuildSearchFile(ctx, tmp); err != nil {
		removeDatabase(tmp)
		return fmt.Errorf("couldn't rebuild search in %s, so nothing changed: %w", name, err)
	}
	aside, err := swapDatabase(ctx, cfg, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if line := savedLine(cfg, aside); line != "" {
		fmt.Fprintf(c.stdout, "Restored %s. %s\n", name, line)
	} else {
		fmt.Fprintf(c.stdout, "Restored %s.\n", name)
	}
	return nil
}

// swapDatabase puts the closed database file at newPath in place of the
// current one, with the lock held. The current database is saved first: as
// a snapshot in {data dir}/backups (aside is its path) or, when it can't be
// copied (damaged, which is often why a restore is wanted, or too big for
// the free space), moved aside whole (aside is where it went). With no
// current database there is nothing to save and aside is "". On an error,
// newPath is still the caller's to remove; once the current database has
// been saved or moved, the error is a *swapFailed saying where it is.
func swapDatabase(ctx context.Context, cfg config.Config, newPath string) (aside string, err error) {
	dir := filepath.Join(cfg.DataDir, "backups")
	moved := false
	if _, err := os.Stat(cfg.DBPath); err == nil {
		kind := appdb.KindPreRestore
		if strings.HasSuffix(newPath, importFile) {
			kind = appdb.KindPreImport
		}
		name, err := snapshotFile(ctx, cfg.DBPath, dir, kind)
		if err == nil {
			aside = filepath.Join(dir, name)
		} else {
			what := "restore"
			if strings.HasSuffix(newPath, importFile) {
				what = "import"
			}
			aside = cfg.DBPath + ".before-" + what + "-" + timefmt.FileStamp(localNow())
			if err := moveDatabase(cfg.DBPath, aside); err != nil {
				return "", err
			}
			moved = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return installDatabase(cfg, newPath, aside, moved)
}

// installDatabase renames the closed database file at newPath over the
// current one, whose copy the caller has saved at aside (moved: moved there
// whole). An error after that is a *swapFailed saying where it is.
func installDatabase(cfg config.Config, newPath, aside string, moved bool) (string, error) {
	fail := func(err error) (string, error) {
		if aside == "" {
			return "", err
		}
		return aside, &swapFailed{err: err, aside: aside, dbPath: cfg.DBPath, moved: moved}
	}
	// A stale WAL would be replayed onto the new file.
	for _, ext := range []string{"-wal", "-shm"} {
		if err := os.Remove(cfg.DBPath + ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
	}
	if err := os.Rename(newPath, cfg.DBPath); err != nil {
		return fail(err)
	}
	// Make the rename itself durable; the swap has happened either way, so
	// a failure here isn't one.
	_ = syncDir(filepath.Dir(cfg.DBPath))
	return aside, nil
}

// syncDir flushes a directory's entries (a rename in it) to disk.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// moveDatabase renames the database at from, with its WAL and shared
// memory, to to. If one rename fails, those done are undone, so on an error
// the database is where it was; when even that fails, the error says where
// it is.
func moveDatabase(from, to string) error {
	var done []string
	for _, ext := range []string{"", "-wal", "-shm"} {
		err := os.Rename(from+ext, to+ext)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil {
			done = append(done, ext)
			continue
		}
		for _, d := range done {
			if uerr := os.Rename(to+d, from+d); uerr != nil {
				return &swapFailed{err: err, aside: to, dbPath: from, moved: true}
			}
		}
		return fmt.Errorf("couldn't copy or move aside the current database, so nothing changed: %w", err)
	}
	return nil
}

// swapFailed is a swap that failed after the current database was saved
// or moved aside: the database may no longer be where it was, so the
// message says where it is and how to put it back.
type swapFailed struct {
	err           error
	aside, dbPath string
	moved         bool // aside is the moved file, not a snapshot
}

func (e *swapFailed) Error() string {
	msg := "The new database couldn't be put in place (" + e.err.Error() + "). "
	if e.moved {
		// moveDatabase moves the write-ahead log and shared memory too, and
		// the log may hold the newest changes: all three go back together.
		return msg + "The database before it is at " + e.aside + ", with any " + e.aside + "-wal and " + e.aside + "-shm files; move them back to " +
			e.dbPath + ", " + e.dbPath + "-wal and " + e.dbPath + "-shm to undo."
	}
	name := filepath.Base(e.aside)
	return msg + "The database before it is saved as " + name + "; vaquero backup restore " + name + " puts it back."
}

func (e *swapFailed) Unwrap() error { return e.err }

// savedLine is the sentence saying where swapDatabase put the database
// before it, or "" when there was none.
func savedLine(cfg config.Config, aside string) string {
	switch {
	case aside == "":
		return ""
	case filepath.Dir(aside) == filepath.Join(cfg.DataDir, "backups"):
		return "The database before it is saved as " + filepath.Base(aside) + "."
	}
	return "The database before it couldn't be copied, so it was moved to " + aside + ", with any -wal and -shm files beside it."
}

// snapshotFile saves the database at path as a snapshot in dir and returns
// its name.
func snapshotFile(ctx context.Context, path, dir string, kind appdb.Kind) (string, error) {
	cur, err := appdb.Open(path)
	if err != nil {
		return "", err
	}
	defer cur.Close()
	info, err := appdb.SnapshotNext(ctx, cur, dir, kind, localNow())
	if err != nil {
		return "", err
	}
	// Each restore or import adds one, so they are pruned as they are made
	// (best effort: the snapshot is what matters).
	_, _ = appdb.PruneBackups(dir, appdb.BackupKeep)
	return info.Name, nil
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
