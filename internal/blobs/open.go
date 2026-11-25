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
func Location(cfg config.Config) string {
	switch cfg.BlobBackend {
	case "dir":
		dir := absClean(cfg.BlobDir)
		if cfg.DataDir != "" {
			if rel, err := filepath.Rel(absClean(cfg.DataDir), dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return DataDirMarker + "/" + filepath.ToSlash(rel)
			}
		}
		return dir
	case "s3":
		return strings.ToLower(strings.TrimSuffix(cfg.S3Endpoint, "/")) + "/" + cfg.S3Bucket
	}
	return ""
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
