package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// BackupNamePattern is the exact shape of a snapshot's file name. Every name
// that arrives from a request is checked against it before any path is built,
// so nothing can reach outside the backup directory.
var BackupNamePattern = regexp.MustCompile(`^vaquero-\d{8}-\d{6}\.sqlite$`)

// MinBackupMargin is the least free space that must remain on the volume
// after a snapshot (see RequiredFree).
const MinBackupMargin = 100 << 20

var (
	// ErrBadBackupName is a name that isn't a snapshot's file name.
	ErrBadBackupName = errors.New("not a snapshot name")
	// ErrBackupNotFound is a valid name with no snapshot behind it.
	ErrBackupNotFound = errors.New("no such snapshot")
	// ErrBackupExists is a second snapshot in the same second.
	ErrBackupExists = errors.New("a snapshot with this name already exists")
	// ErrLowSpace is a snapshot refused for want of room on the volume.
	ErrLowSpace = errors.New("not enough free space for a snapshot")
)

// BackupInfo describes one snapshot file.
type BackupInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// backupStamp is the layout of the instant in a snapshot's file name. It
// lives only here: BackupName formats it and ParseBackupName reads it back.
const backupStamp = "20060102-150405"

// BackupName is the snapshot file name for an instant.
func BackupName(t time.Time) string {
	return "vaquero-" + t.UTC().Format(backupStamp) + ".sqlite"
}

// ParseBackupName reads the instant (UTC) a snapshot's file name was taken
// at. A name that isn't a snapshot's file name is ErrBadBackupName.
func ParseBackupName(name string) (time.Time, error) {
	if !BackupNamePattern.MatchString(name) {
		return time.Time{}, ErrBadBackupName
	}
	return time.Parse(backupStamp, strings.TrimSuffix(strings.TrimPrefix(name, "vaquero-"), ".sqlite"))
}

// snapshotMu keeps two snapshots from racing for the same name or temp file.
var snapshotMu sync.Mutex

// StaleTempAge is how old a leftover temporary snapshot must be before the
// next Snapshot removes it: older than the longest a snapshot can take.
const StaleTempAge = 5 * time.Minute

// Snapshot writes a consistent single-file copy of the database into dir
// with VACUUM INTO and returns it. src is any pool on the database file (the
// reader pool is the usual one); it is used only to find the file and its
// size. The copy runs on its own short-lived connection to the same file, so
// it takes a read transaction under WAL: writers, and the writer pool's one
// connection, carry on while it runs.
//
// It refuses (ErrLowSpace) when the volume would be left with less than
// RequiredFree of the current database size. The copy is written under a
// temporary name, and renamed on success,
// so a failure leaves no partial snapshot in the list; temporary files left
// by a crash and older than StaleTempAge are removed first. It does not
// prune; the caller does, after success.
func Snapshot(ctx context.Context, src *sql.DB, dir string, now time.Time) (BackupInfo, error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return BackupInfo{}, fmt.Errorf("create backup dir: %w", err)
	}
	removeStaleTemps(dir, now)
	name := BackupName(now)
	final := filepath.Join(dir, name)
	if _, err := os.Lstat(final); err == nil {
		return BackupInfo{}, ErrBackupExists
	}
	path, err := mainFile(ctx, src)
	if err != nil {
		return BackupInfo{}, err
	}
	size, err := Size(ctx, src)
	if err != nil {
		return BackupInfo{}, err
	}
	free, err := freeBytes(dir)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("check free space: %w", err)
	}
	if need := RequiredFree(size); free < need {
		return BackupInfo{}, fmt.Errorf("%w: %d bytes free, %d needed", ErrLowSpace, free, need)
	}
	tmp := filepath.Join(dir, tempPrefix+name)
	_ = os.Remove(tmp) // a leftover from a crash
	if err := vacuumInto(ctx, path, tmp); err != nil {
		cleanTemp(tmp)
		return BackupInfo{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		cleanTemp(tmp)
		return BackupInfo{}, fmt.Errorf("finish snapshot: %w", err)
	}
	st, err := os.Stat(final)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Name: name, Size: st.Size(), ModTime: st.ModTime()}, nil
}

const tempPrefix = ".tmp-"

// tempPattern matches a temporary snapshot name.
var tempPattern = regexp.MustCompile(`^\.tmp-vaquero-\d{8}-\d{6}\.sqlite$`)

// removeStaleTemps deletes temporary snapshots a crash left behind. A file
// younger than StaleTempAge is left alone.
func removeStaleTemps(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !tempPattern.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > StaleTempAge {
			cleanTemp(filepath.Join(dir, e.Name()))
		}
	}
}

// cleanTemp removes a temporary snapshot and any journal beside it.
func cleanTemp(p string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(p + suffix)
	}
}

// mainFile is the path of the database file behind a pool.
func mainFile(ctx context.Context, q *sql.DB) (string, error) {
	rows, err := q.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" && file != "" {
			return file, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", errors.New("the database has no file to copy")
}

// openCopyConn opens one connection to a file, for a snapshot's own use.
func openCopyConn(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	p := url.Values{}
	p.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", "file:"+abs+"?"+p.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// vacuumInto copies the database at path to dest on a connection of its own.
var vacuumInto = func(ctx context.Context, path, dest string) error {
	c, err := openCopyConn(path)
	if err != nil {
		return fmt.Errorf("open for snapshot: %w", err)
	}
	defer c.Close()
	if _, err := c.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// Size is the database's size in bytes by its page count, the estimate of
// what a snapshot will take.
func Size(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int64, error) {
	var pages, pageSize int64
	if err := q.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return 0, err
	}
	if err := q.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	return pages * pageSize, nil
}

// RequiredFree is the free space a snapshot of a database this size needs: the
// copy itself plus a margin of the database's size, or MinBackupMargin if
// that is more, left for the live database and its write-ahead log to grow.
func RequiredFree(dbSize int64) int64 {
	return dbSize + max(dbSize, MinBackupMargin)
}

// freeBytes is FreeBytes, replaceable by a test.
var freeBytes = FreeBytes

// FreeBytes is the space available to an unprivileged writer on the
// filesystem holding path. A directory that doesn't exist yet is measured
// through its nearest existing parent.
func FreeBytes(path string) (int64, error) {
	free, _, err := VolumeSpace(path)
	return free, err
}

// VolumeSpace is the free space available to an unprivileged writer and the
// total size of the filesystem holding path (statfs, so it builds on Linux
// and macOS alike). A directory that doesn't exist yet is measured through
// its nearest existing parent.
func VolumeSpace(path string) (free, total int64, err error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}
	for {
		var st syscall.Statfs_t
		err := syscall.Statfs(p, &st)
		if err == nil {
			bsize := int64(st.Bsize)                                       //nolint:gosec // block sizes fit int64
			return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, nil //nolint:gosec // block counts fit int64
		}
		parent := filepath.Dir(p)
		if !errors.Is(err, syscall.ENOENT) || parent == p {
			return 0, 0, err
		}
		p = parent
	}
}

// LowFreeBytes and LowFreeShare are when the volume counts as running low
// (SpaceLow): under 200 MB free, or under a fifth of the volume free. The
// Overview tells admins then; the volume stops every write when full.
const (
	LowFreeBytes = 200 << 20
	LowFreeShare = 5 // free * LowFreeShare < total
)

// SpaceLow reports whether free bytes of a volume of total bytes is low. An
// unknown total (0) is judged by the byte floor alone.
func SpaceLow(free, total int64) bool {
	return free < LowFreeBytes || (total > 0 && free*LowFreeShare < total)
}

// EffectiveKeep caps the configured snapshot count so that the live database
// and its snapshots (about (keep+2) x size at the peak: the live file and its
// log, keep kept, one being written) fit in half the volume, and never goes
// below 1. A database or volume of unknown size leaves keep as set. It only
// lowers the count: the keep count stays the ceiling.
func EffectiveKeep(keep int, size, total int64) int {
	if size <= 0 || total <= 0 {
		return keep
	}
	fit := total/2/size - 2
	return int(max(1, min(int64(keep), fit)))
}

// KeepFor is EffectiveKeep for the database behind src and the volume
// holding dir; where either can't be read it returns keep.
func KeepFor(ctx context.Context, src interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, dir string, keep int) int {
	size, err := Size(ctx, src)
	if err != nil {
		return keep
	}
	_, total, err := VolumeSpace(dir)
	if err != nil {
		return keep
	}
	return EffectiveKeep(keep, size, total)
}

// ListBackups returns the snapshots in dir, newest first. A missing dir is an
// empty list. Anything that isn't a regular file with a snapshot's name
// (symlinks, directories, temporary files) is left out.
func ListBackups(dir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []BackupInfo
	for _, e := range entries {
		if !e.Type().IsRegular() || !BackupNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	slices.SortFunc(out, func(a, b BackupInfo) int { return strings.Compare(b.Name, a.Name) })
	return out, nil
}

// backupPath validates a name and returns its path, which is a regular file
// inside dir (a symlink is refused, so a planted link can't leak a file).
func backupPath(dir, name string) (string, os.FileInfo, error) {
	if !BackupNamePattern.MatchString(name) {
		return "", nil, ErrBadBackupName
	}
	p := filepath.Join(dir, name)
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !st.Mode().IsRegular()) {
		return "", nil, ErrBackupNotFound
	}
	if err != nil {
		return "", nil, err
	}
	return p, st, nil
}

// OpenBackup opens a snapshot for reading, for download.
func OpenBackup(dir, name string) (*os.File, BackupInfo, error) {
	p, _, err := backupPath(dir, name)
	if err != nil {
		return nil, BackupInfo{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, BackupInfo{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, BackupInfo{}, err
	}
	return f, BackupInfo{Name: name, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// DeleteBackup removes one snapshot.
func DeleteBackup(dir, name string) error {
	p, _, err := backupPath(dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); errors.Is(err, os.ErrNotExist) {
		return ErrBackupNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// PruneBackups removes the oldest snapshots until at most keep remain and
// returns the names removed. keep below 1 removes nothing.
func PruneBackups(dir string, keep int) ([]string, error) {
	if keep < 1 {
		return nil, nil
	}
	all, err := ListBackups(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, b := range all[min(keep, len(all)):] {
		if err := DeleteBackup(dir, b.Name); err != nil && !errors.Is(err, ErrBackupNotFound) {
			return removed, err
		}
		removed = append(removed, b.Name)
	}
	return removed, nil
}
