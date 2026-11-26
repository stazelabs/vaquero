package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EnvFileName is the settings file in the data directory, read by serve and
// every command after the data directory is resolved: KEY=value lines with
// the environment's names, which a service manager's restart keeps (VAQ-148).
const EnvFileName = "vaquero.env"

// EnvFilePath is the settings file's path in dataDir.
func EnvFilePath(dataDir string) string { return filepath.Join(dataDir, EnvFileName) }

// Sources of a setting, as Config.Source names them.
const (
	SourceEnvironment = "environment"
	SourceFile        = "file"
	SourceDefault     = "default"
)

// Known are the settings Vaquero reads, in the order a written file lists
// them. A VQ_ key in the file outside this list is a warning, not a
// refusal (VQ_DATA_DIR is refused).
var Known = []string{
	"VQ_MODE", "VQ_ADDR", "VQ_LOG_FORMAT", "VQ_LOG_LEVEL",
	"VQ_BLOB_STORE", "VQ_BLOB_DIR",
	"VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY", "VQ_S3_REGION",
}

// ReadEnvFile reads {dataDir}/vaquero.env. A missing file is no settings
// and no error. The file is refused when its mode lets group or other
// users read it (it may hold the S3 secret) or change it, when a line isn't KEY=value,
// when a key isn't a VQ_ setting, and when it sets VQ_DATA_DIR, which
// would have to be known before the file is found.
//
// Format: one KEY=value per line, the value optionally in double quotes
// (taken as is, with no escapes); spaces around the key and value are
// dropped; a line starting with # is a comment; blank lines are skipped.
// A key set twice takes the later value. Everything after the first = is
// the value, a trailing "# note" included. Errors never quote a value.
func ReadEnvFile(dataDir string) (map[string]string, error) {
	path := EnvFilePath(dataDir)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can't read %s: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("can't read %s: %w", path, err)
	}
	if st.Mode().Perm()&0o044 != 0 {
		//lint:ignore ST1005 a sentence the operator reads as is
		return nil, fmt.Errorf("vaquero.env can be read by other users; run chmod 600 %s.", path)
	}
	// P-R6: one others could change could point the storage elsewhere.
	if st.Mode().Perm()&0o022 != 0 {
		//lint:ignore ST1005 a sentence the operator reads as is
		return nil, fmt.Errorf("vaquero.env can be changed by other users; run chmod 600 %s.", path)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("can't read %s: %w", path, err)
	}
	return ParseEnvFile(path, string(b))
}

// ParseEnvFile parses text, the contents of the file at path (named in
// errors), as ReadEnvFile describes.
func ParseEnvFile(path, text string) (map[string]string, error) {
	vals := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			//lint:ignore ST1005 a sentence the operator reads as is
			return nil, fmt.Errorf("Line %d of %s isn't a KEY=value setting.", n, path)
		}
		if !validKey(key) {
			// Not a key at all (a pasted secret, say): quote nothing.
			//lint:ignore ST1005 a sentence the operator reads as is
			return nil, fmt.Errorf("Line %d of %s doesn't set a VQ_ setting.", n, path)
		}
		if !strings.HasPrefix(key, "VQ_") {
			//lint:ignore ST1005 a sentence the operator reads as is
			return nil, fmt.Errorf("Line %d of %s sets %s; vaquero.env takes only VQ_ settings.", n, path, key)
		}
		if key == "VQ_DATA_DIR" {
			//lint:ignore ST1005 the ruled message
			return nil, errors.New("VQ_DATA_DIR can't be set in vaquero.env.")
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		vals[key] = val
	}
	return vals, nil
}

// validKey: upper-case ASCII letters, digits and underscores, at most 64,
// the first a letter. Only such a key is ever named in a message.
func validKey(k string) bool {
	if len(k) > 64 || k[0] < 'A' || k[0] > 'Z' {
		return false
	}
	for _, r := range k {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// unknownKeys are the file's VQ_ keys Vaquero doesn't read, sorted.
func unknownKeys(file map[string]string) []string {
	var out []string
	for k := range file {
		if !slices.Contains(Known, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Layered looks a key up in the environment (get), then in the file's
// values: a variable set in the environment overrides the file, and one
// set to nothing there (VQ_BLOB_DIR=) falls through to the file, not to
// the default.
func Layered(get func(string) string, file map[string]string) func(string) string {
	return func(k string) string {
		if v := get(k); v != "" {
			return v
		}
		return file[k]
	}
}

// sourceOf names where key's value comes from for get layered over file.
func sourceOf(key string, get func(string) string, file map[string]string) string {
	if get(key) != "" {
		return SourceEnvironment
	}
	if file[key] != "" {
		return SourceFile
	}
	return SourceDefault
}
