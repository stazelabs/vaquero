// Package config reads the VQ_* environment into one flat Config.
//
// No struct tags and no reflection: FromEnv reads each variable by name,
// checks it, and reports every problem at once.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
}

// Load reads .env.local when present (best effort, for development), then
// the process environment.
func Load() (Config, error) {
	_ = godotenv.Load(".env.local")
	return FromEnv(os.Getenv)
}

// FromEnv builds a Config from get, which looks up one variable.
func FromEnv(get func(string) string) (Config, error) {
	c := Config{
		Mode:      Mode(or(get("VQ_MODE"), string(ModeLocal))),
		Addr:      or(get("VQ_ADDR"), "127.0.0.1:7878"),
		DataDir:   get("VQ_DATA_DIR"),
		LogFormat: or(get("VQ_LOG_FORMAT"), "text"),
		LogLevel:  or(get("VQ_LOG_LEVEL"), "info"),
	}
	var errs []error
	switch c.Mode {
	case ModeLocal:
	case ModeHosted:
		errs = append(errs, errors.New("VQ_MODE=hosted is not available yet; this build runs in local mode only"))
	default:
		errs = append(errs, fmt.Errorf("VQ_MODE must be local or hosted, not %q", c.Mode))
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		errs = append(errs, fmt.Errorf("VQ_LOG_FORMAT must be text or json, not %q", c.LogFormat))
	}
	if err := checkAddr(c.Mode, c.Addr); err != nil {
		errs = append(errs, err)
	}
	if c.DataDir == "" {
		dir, err := DefaultDataDir(runtime.GOOS, get)
		if err != nil {
			errs = append(errs, err)
		}
		c.DataDir = dir
	}
	c.DBPath = filepath.Join(c.DataDir, "vaquero.db")
	return c, errors.Join(errs...)
}

// checkAddr rejects an unparseable address and, in local mode, any address
// that is not loopback: local mode has no sign-in, so it must never be
// reachable from the network.
func checkAddr(mode Mode, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("VQ_ADDR %q is not host:port: %w", addr, err)
	}
	if mode != ModeLocal {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("VQ_ADDR must be a loopback address (127.0.0.1, localhost or [::1]) in local mode; sharing on a network needs hosted mode")
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
