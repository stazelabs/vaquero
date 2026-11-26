package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
)

// writeEnvFile merges set into {dataDir}/vaquero.env: each key in set
// replaces that key's first line (its later lines go) or is added at the
// end, in config.Known order; a key set to "" is removed. Comments, blank
// lines and every other setting stay as they were. The file is written
// through a synced temp file at mode 0600, then renamed into place, so a
// kill leaves the old file or the new one, never half of either. A
// vaquero.env that is a symlink is replaced by a regular file; the file it
// pointed to is left as it was.
func writeEnvFile(dataDir string, set map[string]string) error {
	path := config.EnvFilePath(dataDir)
	for k, v := range set {
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("%s can't be written to %s: its value has a line break", k, path)
		}
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out []string
	seen := map[string]bool{}
	text := strings.TrimSuffix(string(old), "\n")
	if text != "" {
		for line := range strings.SplitSeq(text, "\n") {
			key := envLineKey(line)
			v, ours := set[key]
			switch {
			case !ours:
				out = append(out, line)
			case seen[key] || v == "":
				// a repeat, or removed
			default:
				out = append(out, envLine(key, v))
			}
			if ours {
				seen[key] = true
			}
		}
	}
	keys := slices.SortedFunc(maps.Keys(set), func(a, b string) int {
		ia, ib := knownIndex(a), knownIndex(b)
		if ia != ib {
			return ia - ib
		}
		return strings.Compare(a, b)
	})
	for _, k := range keys {
		if !seen[k] && set[k] != "" {
			out = append(out, envLine(k, set[k]))
		}
	}
	body := strings.Join(out, "\n")
	if body != "" {
		body += "\n"
	}
	return writeSynced(path, []byte(body))
}

// knownIndex is k's place in config.Known, unknown keys last.
func knownIndex(k string) int {
	if i := slices.Index(config.Known, k); i >= 0 {
		return i
	}
	return len(config.Known)
}

// envLineKey is the key a vaquero.env line sets, or "" for a comment, a
// blank line or a line that isn't KEY=value.
func envLineKey(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	k, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(k)
}

// envLine is KEY=value, the value in double quotes unless it is plain
// (letters, digits and . _ / : @ + , = -), which config.ParseEnvFile reads
// back as written.
func envLine(key, v string) string {
	plain := v != ""
	for _, r := range v {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._/:@+,=-", r) {
			plain = false
			break
		}
	}
	if plain {
		return key + "=" + v
	}
	return key + `="` + v + `"`
}

// writeSynced writes b to path at 0600 through path.tmp: written, synced,
// closed, renamed, then the directory synced. A .tmp left by an earlier
// run is removed first, so its mode is never kept.
func writeSynced(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// storageSettings are the settings serve needs for cfg's storage, for
// vaquero.env. With dropStale the other backends' settings are set to ""
// (removed): the S3 keys after a move off a bucket, VQ_BLOB_DIR after a
// move off a directory. A move keeps them while it still has old copies to
// delete, since the rerun that deletes them reads the old storage's
// settings from the file.
func storageSettings(cfg config.Config, dropStale bool) map[string]string {
	set := map[string]string{"VQ_BLOB_STORE": cfg.BlobBackend}
	s3 := map[string]string{
		"VQ_S3_ENDPOINT": cfg.S3Endpoint, "VQ_S3_BUCKET": cfg.S3Bucket,
		"VQ_S3_ACCESS_KEY_ID": cfg.S3AccessKeyID, "VQ_S3_SECRET_ACCESS_KEY": string(cfg.S3SecretAccessKey),
		"VQ_S3_REGION": cfg.S3Region,
	}
	switch cfg.BlobBackend {
	case "dir":
		// The default directory is left out, so it moves with the data
		// directory as blob_location's {data dir}/attachments does; any
		// other directory is written as its absolute path.
		if blobs.Location(cfg) == defaultBlobLocation {
			set["VQ_BLOB_DIR"] = ""
		} else {
			set["VQ_BLOB_DIR"] = cfg.BlobDir
		}
	case "s3":
		maps.Copy(set, s3)
	}
	if dropStale {
		if cfg.BlobBackend != "dir" {
			set["VQ_BLOB_DIR"] = ""
		}
		if cfg.BlobBackend != "s3" {
			for k := range s3 {
				set[k] = ""
			}
		}
	}
	return set
}

// defaultBlobLocation is blobs.Location of {data dir}/attachments.
const defaultBlobLocation = blobs.DataDirMarker + "/attachments"

// envFileHolds reports whether vaquero.env already says set: each key at
// its value, and each key set to "" absent.
func envFileHolds(cfg config.Config, set map[string]string) bool {
	vals, err := config.ReadEnvFile(cfg.DataDir)
	if err != nil {
		return false
	}
	for k, v := range set {
		if vals[k] != v {
			return false
		}
	}
	return true
}

// shownSettings is shown, settings already through config.Redact, as a
// sentence's list in config.Known order: "VQ_BLOB_STORE=dir, VQ_BLOB_DIR=/x".
func shownSettings(shown map[string]string) string {
	var parts []string
	for _, k := range config.Known {
		if v := shown[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, ", ")
}
