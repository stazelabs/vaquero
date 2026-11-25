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

// Location names where the configured store keeps its bytes, recorded in
// settings beside the backend (blob_location) so a boot pointed at another
// directory or bucket is caught: the absolute directory for dir, endpoint
// and bucket for s3 ("endpoint/bucket", the endpoint in lower case), and ""
// for sqlite, whose bytes are in the database itself.
func Location(cfg config.Config) string {
	switch cfg.BlobBackend {
	case "dir":
		if abs, err := filepath.Abs(cfg.BlobDir); err == nil {
			return abs
		}
		return filepath.Clean(cfg.BlobDir)
	case "s3":
		return strings.ToLower(strings.TrimSuffix(cfg.S3Endpoint, "/")) + "/" + cfg.S3Bucket
	}
	return ""
}
