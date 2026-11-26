package blobs

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
)

// Open builds the Store the configuration names. The dir backend creates
// BlobDir (mode 0700); the sqlite backend uses the app's own database pools.
// A bad directory or S3 setting fails here, at boot, not at the first upload.
func Open(cfg config.Config, w, r *sql.DB, now func() time.Time) (Store, error) {
	switch cfg.BlobBackend {
	case "sqlite":
		return NewSQLite(w, r, now), nil
	case "dir":
		return NewDir(cfg.BlobDir)
	case "s3":
		return NewS3(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, string(cfg.S3SecretAccessKey), cfg.S3Region)
	default:
		return nil, fmt.Errorf("blobs: unknown backend %q", cfg.BlobBackend)
	}
}

// DataDirMarker starts a dir location inside the data directory.
const DataDirMarker = "{data dir}"

// Location names where the configured store keeps its bytes, recorded in
// settings beside the backend (blob_location) so a boot pointed at another
// directory or bucket is caught. For dir it is the directory relative to the
// data directory when inside it ("{data dir}/attachments", so a data
// directory moved whole keeps matching), else absolute; for s3,
// "endpoint/bucket" with the endpoint in lower case; for sqlite "", since
// its bytes are in the database itself.
//
// Inside is decided first as spelled (Abs plus Clean), then with symlinks
// resolved on both directories (realPath), so a data directory reached
// through a symlink in one setting and by its real path in the other still
// counts. A dir outside is recorded as spelled, never resolved, so what an
// unchanged setup records stays the same.
func Location(cfg config.Config) string {
	switch cfg.BlobBackend {
	case "dir":
		dir := absClean(cfg.BlobDir)
		if cfg.DataDir != "" {
			if rel, ok := inside(absClean(cfg.DataDir), dir); ok {
				return DataDirMarker + "/" + rel
			}
			if rel, ok := inside(realPath(cfg.DataDir), realPath(cfg.BlobDir)); ok {
				return DataDirMarker + "/" + rel
			}
		}
		return dir
	case "s3":
		return strings.ToLower(strings.TrimSuffix(cfg.S3Endpoint, "/")) + "/" + cfg.S3Bucket
	}
	return ""
}

// SameLocation reports whether recorded (a blob_location setting) names the
// place cfg points at: the same string as Location, or, for dir, the same
// directory once symlinks are resolved. That keeps a location recorded
// before Location resolved symlinks (absolute, through a symlink) from
// refusing a boot whose directory hasn't changed.
func SameLocation(recorded string, cfg config.Config) bool {
	now := Location(cfg)
	if recorded == now {
		return true
	}
	if cfg.BlobBackend != "dir" || recorded == "" {
		return false
	}
	was := ResolveLocation(recorded, cfg.DataDir)
	if !filepath.IsAbs(was) {
		return false
	}
	return realPath(was) == realPath(cfg.BlobDir)
}

func inside(base, dir string) (string, bool) {
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// realPath is p with symlinks resolved. A path that doesn't exist yet (a
// blob dir before its first boot) resolves its nearest existing parent and
// keeps the rest as spelled; with no parent to resolve it is Abs plus Clean.
func realPath(p string) string {
	abs := absClean(p)
	rest := ""
	for cur := abs; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// ResolveLocation turns a dir location back into a path under dataDir, for
// messages; any other location is returned as is.
func ResolveLocation(loc, dataDir string) string {
	if rel, ok := strings.CutPrefix(loc, DataDirMarker+"/"); ok {
		return filepath.Join(absClean(dataDir), filepath.FromSlash(rel))
	}
	return loc
}

func absClean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
