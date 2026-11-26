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

// Kind says why a snapshot was taken. It is part of the file name
// ("vaquero-predelete-20261013-093005.sqlite") so that each kind is kept and
// pruned on its own: a run of project deletes can't push out the snapshot
// taken before an upgrade. A snapshot with no kind in its name (every one
// from before kinds, and Snapshot's own) is Kind "" and is never pruned.
type Kind string

const (
	KindPreMigrate Kind = "premigrate"
	KindPreDelete  Kind = "predelete"
	KindPreRestore Kind = "prerestore"
	KindPreImport  Kind = "preimport"
	KindPreCompact Kind = "precompact"
	KindPreMove    Kind = "premove"
	// KindManual is a snapshot taken from the admin backups page.
	KindManual Kind = "manual"
)

// BackupNamePattern is the exact shape of a snapshot's file name. Every name
// that arrives from a request is checked against it before any path is built,
// so nothing can reach outside the backup directory.
var BackupNamePattern = regexp.MustCompile(`^vaquero-(?:(premigrate|predelete|prerestore|preimport|precompact|premove|manual)-)?(\d{8}-\d{6})\.sqlite$`)

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

// BackupInfo describes one snapshot file. Taken is the instant in its
// name (UTC); ModTime is the file's.
type BackupInfo struct {
	Name    string
	Kind    Kind
	Size    int64
	ModTime time.Time
	Taken   time.Time
}

// takenAt is the instant in a valid snapshot name, zero if there is none.
func takenAt(name string) time.Time {
	_, stamp, ok := kindAndStamp(name)
	if !ok {
		return time.Time{}
	}
	t, err := time.ParseInLocation(backupStamp, stamp, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// backupStamp is the layout of the instant in a snapshot's file name.
const backupStamp = "20060102-150405"

// BackupName is the plain (kindless) snapshot file name for an instant.
func BackupName(t time.Time) string { return BackupNameKind("", t) }

// BackupNameKind is the snapshot file name for a kind and an instant.
func BackupNameKind(k Kind, t time.Time) string {
	if k == "" {
		return "vaquero-" + t.UTC().Format(backupStamp) + ".sqlite"
	}
	return "vaquero-" + string(k) + "-" + t.UTC().Format(backupStamp) + ".sqlite"
}

// kindAndStamp splits a valid snapshot name into its kind and timestamp text.
func kindAndStamp(name string) (Kind, string, bool) {
	m := BackupNamePattern.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return Kind(m[1]), m[2], true
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
// temporary name and renamed on success, so a failure leaves no partial
// snapshot in the list; temporary files left by a crash and older than
// StaleTempAge are removed first. It does not prune; the caller does, after
// success. Snapshot's file has no kind in its name; SnapshotNext takes one.
func Snapshot(ctx context.Context, src *sql.DB, dir string, now time.Time) (BackupInfo, error) {
	return snapshot(ctx, src, dir, "", now)
}

func snapshot(ctx context.Context, src *sql.DB, dir string, kind Kind, now time.Time) (BackupInfo, error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return BackupInfo{}, fmt.Errorf("create backup dir: %w", err)
	}
	removeStaleTemps(dir, now)
	name := BackupNameKind(kind, now)
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
	return BackupInfo{Name: name, Kind: kind, Size: st.Size(), ModTime: st.ModTime(), Taken: takenAt(name)}, nil
}

const tempPrefix = ".tmp-"

// tempPattern matches a temporary snapshot name.
var tempPattern = regexp.MustCompile(`^\.tmp-vaquero-(?:(?:premigrate|predelete|prerestore|preimport|precompact|premove|manual)-)?\d{8}-\d{6}\.sqlite$`)

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
// filesystem holding path (statfs, so it builds on Linux and macOS alike). A
// directory that doesn't exist yet is measured through its nearest existing
// parent.
func FreeBytes(path string) (int64, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	for {
		var st syscall.Statfs_t
		err := syscall.Statfs(p, &st)
		if err == nil {
			return int64(st.Bavail) * blockSize(&st), nil //nolint:gosec // block counts fit int64
		}
		parent := filepath.Dir(p)
		if !errors.Is(err, syscall.ENOENT) || parent == p {
			return 0, err
		}
		p = parent
	}
}

// snapshotVersion is the schema version a snapshot records, read without
// writing to it; ok is false when it can't be read.
func snapshotVersion(path string) (v int64, ok bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	// immutable: a snapshot is never written, and a read-only open of a
	// WAL-mode file would otherwise need its shared memory.
	d, err := sql.Open("sqlite", "file:"+abs+"?mode=ro&immutable=1")
	if err != nil {
		return 0, false
	}
	defer d.Close()
	var got sql.NullInt64
	if err := d.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&got); err != nil || !got.Valid {
		return 0, false
	}
	return got.Int64, true
}

// SameFilesystem reports whether paths a and b are on one filesystem. A
// path that doesn't exist yet is placed by its nearest existing parent.
func SameFilesystem(a, b string) (bool, error) {
	da, err := device(a)
	if err != nil {
		return false, err
	}
	db, err := device(b)
	if err != nil {
		return false, err
	}
	return da == db, nil
}

func device(path string) (uint64, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	for {
		var st syscall.Stat_t
		err := syscall.Stat(p, &st)
		if err == nil {
			return uint64(st.Dev), nil //nolint:gosec,unconvert // int32 on macOS, uint64 on Linux
		}
		parent := filepath.Dir(p)
		if !errors.Is(err, syscall.ENOENT) || parent == p {
			return 0, err
		}
		p = parent
	}
}

// ListBackups returns the snapshots in dir, newest first (by the instant in
// the name, whatever the kind). A missing dir is an empty list. Anything
// that isn't a regular file with a snapshot's name (symlinks, directories,
// temporary files) is left out.
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
		kind, _, _ := kindAndStamp(e.Name())
		out = append(out, BackupInfo{Name: e.Name(), Kind: kind, Size: info.Size(), ModTime: info.ModTime(), Taken: takenAt(e.Name())})
	}
	slices.SortFunc(out, func(a, b BackupInfo) int {
		_, sa, _ := kindAndStamp(a.Name)
		_, sb, _ := kindAndStamp(b.Name)
		if c := strings.Compare(sb, sa); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
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
	kind, _, _ := kindAndStamp(name)
	return f, BackupInfo{Name: name, Kind: kind, Size: st.Size(), ModTime: st.ModTime(), Taken: takenAt(name)}, nil
}

// BackupSchema is the schema version the snapshot called name in dir
// records, read without writing to it. ok is false for a name that isn't a
// snapshot's, a snapshot that isn't there (or isn't a regular file), and
// one that can't be read.
func BackupSchema(dir, name string) (v int64, ok bool) {
	p, _, err := backupPath(dir, name)
	if err != nil {
		return 0, false
	}
	return snapshotVersion(p)
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

// SnapshotNext is Snapshot for a kind, taking the next free second's name
// when one is taken (a snapshot a moment earlier: a restore, then a boot),
// up to five.
func SnapshotNext(ctx context.Context, src *sql.DB, dir string, kind Kind, now time.Time) (BackupInfo, error) {
	for range 5 {
		info, err := snapshot(ctx, src, dir, kind, now)
		if !errors.Is(err, ErrBackupExists) {
			return info, err
		}
		now = now.Add(time.Second)
	}
	return BackupInfo{}, ErrBackupExists
}

// BackupKeep is how many snapshots of each kind are kept.
const BackupKeep = 7

// PruneBackups removes the oldest snapshots of each kind until at most keep
// of that kind remain and returns the names removed. Snapshots with no kind
// in their name (from before kinds) are never removed. Beyond keep, the
// newest pre-migration snapshot of each schema version not already kept
// stays too, so the last copy of an older layout is never pruned (one that
// can't be read is pruned as usual). keep below 1 removes nothing.
func PruneBackups(dir string, keep int) ([]string, error) {
	if keep < 1 {
		return nil, nil
	}
	all, err := ListBackups(dir)
	if err != nil {
		return nil, err
	}
	var removed []string
	seen := map[Kind]int{}
	schemas := map[int64]bool{} // versions of the pre-migration snapshots kept
	for _, b := range all {
		if b.Kind == "" {
			continue
		}
		seen[b.Kind]++
		if b.Kind == KindPreMigrate {
			v, ok := snapshotVersion(filepath.Join(dir, b.Name))
			if ok && (seen[b.Kind] <= keep || !schemas[v]) {
				schemas[v] = true
				continue
			}
		}
		if seen[b.Kind] <= keep {
			continue
		}
		if err := DeleteBackup(dir, b.Name); err != nil && !errors.Is(err, ErrBackupNotFound) {
			return removed, err
		}
		removed = append(removed, b.Name)
	}
	return removed, nil
}
