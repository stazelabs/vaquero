// Package config reads the VQ_* settings into one flat Config: the
// environment, then {data dir}/vaquero.env (envfile.go), then the defaults.
//
// No struct tags and no reflection: FromEnv reads each variable by name,
// checks it, and reports every problem at once.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
)

// Mode says who the instance serves.
type Mode string

const (
	// ModeLocal is one person on one machine: loopback only, no sign-in.
	ModeLocal Mode = "local"
	// ModeHosted is a shared instance behind sign-in (Phase 4).
	ModeHosted Mode = "hosted"
)

// Config is the whole server configuration.
type Config struct {
	Mode      Mode
	Addr      string // listen address; default "127.0.0.1:7878"
	DataDir   string // VQ_DATA_DIR, else DefaultDataDir
	DBPath    string // always filepath.Join(DataDir, "vaquero.db")
	LogFormat string // "text" (default) or "json"
	LogLevel  string // default "info"

	// BlobBackend is VQ_BLOB_STORE: "sqlite" (default), "dir" or "s3".
	BlobBackend string
	// BlobDir is VQ_BLOB_DIR, the dir backend's root; {data dir}/attachments
	// when unset. Unused with the other backends.
	BlobDir string
	// S3Endpoint, S3Bucket, S3AccessKeyID, S3SecretAccessKey and S3Region
	// are VQ_S3_ENDPOINT (host or host:port), VQ_S3_BUCKET,
	// VQ_S3_ACCESS_KEY_ID, VQ_S3_SECRET_ACCESS_KEY and VQ_S3_REGION
	// (default "auto"). The first four are required with s3. The secret is
	// never logged.
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey Secret
	S3Region          string

	// Warnings name the keys vaquero.env sets that Vaquero doesn't read;
	// serve logs them.
	Warnings []string

	// sources names where each setting came from (Source); lookup reads a
	// setting, the environment over the file. Neither prints a value.
	sources map[string]string
	lookup  func(string) string
}

// Source says where key's value came from: SourceEnvironment,
// SourceFile (vaquero.env) or SourceDefault. VQ_DATA_DIR is never from
// the file.
func (c Config) Source(key string) string {
	if s, ok := c.sources[key]; ok {
		return s
	}
	return SourceDefault
}

// Lookup reads one setting as the loaders did: the environment, else
// vaquero.env, else "". For settings a command reads beyond Config (the
// old bucket's keys during a move).
func (c Config) Lookup(key string) string {
	if c.lookup == nil {
		return ""
	}
	return c.lookup(key)
}

// Secret is a credential that prints as "[redacted]" under every fmt verb,
// %#v and slog; read the value with string(s).
type Secret []byte

const redacted = "[redacted]"

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer, for %#v.
func (Secret) GoString() string { return redacted }

// Format implements fmt.Formatter so no verb (%x, %q, %v) shows the bytes.
func (Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// MarshalText implements encoding.TextMarshaler, so JSON and any other text
// encoding carry the placeholder, never the bytes.
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// LogValue implements slog.LogValuer.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// Load reads .env.local when present (best effort, for development), then
// the process environment.
func Load() (Config, error) {
	_ = godotenv.Load(".env.local")
	return FromEnv(os.Getenv)
}

// FromEnv builds the whole Config from get, which looks up one variable,
// layered over {data dir}/vaquero.env, checking every setting: what vaquero
// serve reads.
func FromEnv(get func(string) string) (Config, error) {
	c, errs := local(get)
	get = c.lookup
	c.LogFormat = or(get("VQ_LOG_FORMAT"), "text")
	c.LogLevel = or(get("VQ_LOG_LEVEL"), "info")
	if c.LogFormat != "text" && c.LogFormat != "json" {
		errs = append(errs, fmt.Errorf("VQ_LOG_FORMAT must be text or json, not %q", c.LogFormat))
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("VQ_LOG_LEVEL must be debug, info, warn or error, not %q", c.LogLevel))
	}
	if err := readBlobs(&c, get); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

// Local reads and checks only what the command line needs to find the local
// server and its data directory: VQ_MODE, VQ_ADDR and VQ_DATA_DIR (Mode,
// Addr, DataDir, DBPath). The server-only settings (VQ_LOG_*, VQ_BLOB_*,
// VQ_S3_*) are neither read nor checked, so a bad one stops only serve.
// vaquero.env is read, and a refused file stops every command.
func Local(get func(string) string) (Config, error) {
	c, errs := local(get)
	return c, errors.Join(errs...)
}

// LocalWithBlobs is Local plus the attachment storage settings (VQ_BLOB_*,
// VQ_S3_*), checked: what vaquero import reads.
func LocalWithBlobs(get func(string) string) (Config, error) {
	c, errs := local(get)
	if err := readBlobs(&c, c.lookup); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

// local resolves the data directory from get alone, reads its
// vaquero.env, then reads mode and address from get layered over the file,
// with their problems. c.lookup is the layered lookup for the rest.
func local(get func(string) string) (Config, []error) {
	var errs []error
	c := Config{DataDir: get("VQ_DATA_DIR")}
	dataDirFrom := SourceEnvironment
	if c.DataDir == "" {
		dataDirFrom = SourceDefault
		dir, err := DefaultDataDir(runtime.GOOS, get)
		if err != nil {
			errs = append(errs, err)
		}
		c.DataDir = dir
	}
	c.DBPath = filepath.Join(c.DataDir, "vaquero.db")
	var file map[string]string
	if c.DataDir != "" {
		f, err := ReadEnvFile(c.DataDir)
		if err != nil {
			errs = append(errs, err)
		}
		file = f
	}
	for _, k := range unknownKeys(file) {
		c.Warnings = append(c.Warnings, "vaquero.env sets "+k+", which Vaquero doesn't read.")
	}
	c.sources = map[string]string{"VQ_DATA_DIR": dataDirFrom}
	for _, k := range Known {
		c.sources[k] = sourceOf(k, get, file)
	}
	c.lookup = Layered(get, file)
	get = c.lookup
	c.Mode = Mode(or(get("VQ_MODE"), string(ModeLocal)))
	c.Addr = or(get("VQ_ADDR"), "127.0.0.1:7878")
	switch c.Mode {
	case ModeLocal:
	case ModeHosted:
		errs = append(errs, errors.New("VQ_MODE=hosted is not available yet; this build runs in local mode only"))
	default:
		errs = append(errs, fmt.Errorf("VQ_MODE must be local or hosted, not %q", c.Mode))
	}
	if err := checkAddr(c.Mode, c.Addr); err != nil {
		errs = append(errs, err)
	}
	return c, errs
}

// readBlobs fills the blob store settings. The S3 variables are read only
// with the s3 backend; with the others they are ignored.
func readBlobs(c *Config, get func(string) string) error {
	c.BlobBackend = strings.ToLower(or(strings.TrimSpace(get("VQ_BLOB_STORE")), "sqlite"))
	c.BlobDir = or(strings.TrimSpace(get("VQ_BLOB_DIR")), filepath.Join(c.DataDir, "attachments"))
	abs, err := filepath.Abs(c.BlobDir)
	if err != nil {
		return fmt.Errorf("VQ_BLOB_DIR: %w", err)
	}
	c.BlobDir = abs
	c.S3Region = "auto"
	switch c.BlobBackend {
	case "sqlite", "dir":
		return nil
	case "s3":
	default:
		return fmt.Errorf("VQ_BLOB_STORE must be sqlite, dir or s3, got %q", c.BlobBackend)
	}
	c.S3Endpoint = strings.TrimSpace(get("VQ_S3_ENDPOINT"))
	c.S3Bucket = strings.TrimSpace(get("VQ_S3_BUCKET"))
	c.S3AccessKeyID = strings.TrimSpace(get("VQ_S3_ACCESS_KEY_ID"))
	c.S3SecretAccessKey = Secret(strings.TrimSpace(get("VQ_S3_SECRET_ACCESS_KEY")))
	c.S3Region = or(strings.TrimSpace(get("VQ_S3_REGION")), "auto")
	var missing []string
	for _, f := range []struct {
		name string
		ok   bool
	}{
		{"VQ_S3_ENDPOINT", c.S3Endpoint != ""},
		{"VQ_S3_BUCKET", c.S3Bucket != ""},
		{"VQ_S3_ACCESS_KEY_ID", c.S3AccessKeyID != ""},
		{"VQ_S3_SECRET_ACCESS_KEY", len(c.S3SecretAccessKey) > 0},
	} {
		if !f.ok {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("VQ_BLOB_STORE=s3 needs VQ_S3_ENDPOINT, VQ_S3_BUCKET, VQ_S3_ACCESS_KEY_ID and VQ_S3_SECRET_ACCESS_KEY; missing: %s", strings.Join(missing, ", "))
	}
	if strings.Contains(c.S3Endpoint, "@") {
		return errors.New("VQ_S3_ENDPOINT can't hold a user name or password; set VQ_S3_ACCESS_KEY_ID and VQ_S3_SECRET_ACCESS_KEY instead")
	}
	if ep := c.S3Endpoint; strings.ContainsAny(ep, "/'\"") || strings.ContainsFunc(ep, unicode.IsSpace) {
		return fmt.Errorf("VQ_S3_ENDPOINT must be a host with an optional port, with no https:// and no /bucket path (got %q)", ep)
	}
	return nil
}

// checkAddr rejects an unparseable address, a port outside 1 to 65535 (port
// 0 too: local mode needs a port the CLI and the owner can name) and, in
// local mode, any host the Host check would refuse. Local mode has no
// sign-in, so it must never be reachable from the network, and an address
// that boots but answers 403 to every request is worse than a refusal.
func checkAddr(mode Mode, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("VQ_ADDR %q is not host:port: %w", addr, err)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("VQ_ADDR %q has no usable port: the port must be a number from 1 to 65535", addr)
	}
	if mode != ModeLocal {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && AllowedHostIP(ip) {
		return nil
	}
	return errors.New("VQ_ADDR must be a loopback address the server answers to (localhost, 127.0.0.1 or ::1) in local mode; sharing on a network needs hosted mode")
}

// AllowedHostIP reports whether ip is one of the two loopback addresses the
// Host check answers to, 127.0.0.1 and ::1. Other loopback addresses, such
// as 127.0.0.2, would boot and then be refused on every request.
func AllowedHostIP(ip net.IP) bool {
	return ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback)
}

// DefaultDataDir is where local mode keeps its data when VQ_DATA_DIR is
// unset: ~/Library/Application Support/vaquero on macOS, else
// $XDG_DATA_HOME/vaquero or ~/.local/share/vaquero.
func DefaultDataDir(goos string, get func(string) string) (string, error) {
	home := get("HOME")
	if goos == "darwin" {
		if home == "" {
			return "", errors.New("HOME is not set; set VQ_DATA_DIR")
		}
		return filepath.Join(home, "Library", "Application Support", "vaquero"), nil
	}
	if xdg := get("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "vaquero"), nil
	}
	if home == "" {
		return "", errors.New("HOME is not set; set VQ_DATA_DIR")
	}
	return filepath.Join(home, ".local", "share", "vaquero"), nil
}

// AllowedHosts are the Host header values the server answers to. Any other
// Host is a browser being pointed at loopback from another site.
func (c Config) AllowedHosts() []string {
	return []string{"localhost", "127.0.0.1", "::1"}
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
