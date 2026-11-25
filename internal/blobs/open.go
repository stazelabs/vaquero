package blobs

import (
	"database/sql"
	"fmt"
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
