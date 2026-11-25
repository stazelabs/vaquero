package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
)

// pendingFile, in the data directory, names the instance an import is
// putting attachment bytes under, from before its first put until it
// succeeds or has deleted them again. An import killed in between (SIGKILL,
// a power cut) leaves it, and the next import or serve deletes what that
// instance holds: no database records the instance, so no sweep would.
const pendingFile = "import.pending"

// pendingImport is pendingFile's content.
type pendingImport struct {
	Instance string `json:"instance"`
	Backend  string `json:"backend"`
	Location string `json:"location"` // blobs.Location
}

// writePending records that an import is about to put bytes under
// instance in cfg's store.
func writePending(cfg config.Config, instance string) error {
	b, err := json.Marshal(pendingImport{Instance: instance, Backend: cfg.BlobBackend, Location: blobs.Location(cfg)})
	if err != nil {
		return err
	}
	path := filepath.Join(cfg.DataDir, pendingFile)
	if err := copyFile(path, bytes.NewReader(b)); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("record the import in %s: %w", path, err)
	}
	return syncDir(cfg.DataDir)
}

// removePending removes pendingFile.
func removePending(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, pendingFile))
}

// cleanPending deletes the objects a killed import left, as pendingFile
// names them, and then the file. The caller holds the data directory's lock
// and passes the instance the current database records (never deleted:
// that import finished) and its store for cfg, or nil to open one. Best
// effort: the file stays when a delete fails, so the next run tries again;
// a store other than the configured one can't be reached, and is reported.
// It returns how many objects it deleted.
func cleanPending(ctx context.Context, cfg config.Config, store blobs.Store, current string) (int, error) {
	path := filepath.Join(cfg.DataDir, pendingFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var p pendingImport
	if err := json.Unmarshal(b, &p); err != nil || p.Instance == "" || p.Instance == current || p.Backend == "sqlite" {
		removePending(cfg.DataDir) // unreadable, finished, or bytes that went with the import's database
		return 0, nil
	}
	if p.Backend != cfg.BlobBackend || p.Location != blobs.Location(cfg) {
		removePending(cfg.DataDir)
		return 0, fmt.Errorf("an interrupted import left files under attachments/%s/ in %s %s, which isn't the storage configured now; remove them by hand", p.Instance, p.Backend, p.Location)
	}
	if store == nil {
		if store, err = openBlobStore(cfg, nil, nil, time.Now); err != nil {
			return 0, err
		}
	}
	dctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var keys []string
	if err := store.List(dctx, "attachments/"+p.Instance+"/", func(key string, _ time.Time) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return 0, err
	}
	n := 0
	for _, k := range keys {
		if err := store.Delete(dctx, k); err != nil {
			return n, fmt.Errorf("delete %s, left by an interrupted import: %w", k, err)
		}
		n++
	}
	if cfg.BlobBackend == "dir" {
		_ = os.Remove(filepath.Join(cfg.BlobDir, "attachments", p.Instance)) // only when empty
	}
	removePending(cfg.DataDir)
	return n, nil
}
