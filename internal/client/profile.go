package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stazelabs/vaquero/internal/config"
)

// Profile is a server the CLI can talk to. "local" is implicit: the
// local server and its token file.
type Profile struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	DataDir string `json:"-"` // local only
}

// Config is the CLI's saved profiles.
type Config struct {
	Current  string    `json:"current,omitempty"`
	Profiles []Profile `json:"profiles"`
}

// ConfigPath is where profiles are saved: vaquero/config.json in the
// user's config directory. It holds tokens, so it is written 0600.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vaquero", "config.json"), nil
}

// Load reads the saved profiles; none saved is an empty Config.
func Load() (Config, error) {
	var c Config
	path, err := ConfigPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s isn't valid: %w", path, err)
	}
	return c, nil
}

// Save writes the profiles with mode 0600.
func (c Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Find is the saved profile called name.
func (c Config) Find(name string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// Resolve picks the profile to use: name (a --profile flag) if given, else
// VAQUERO_PROFILE, else the saved current one, else "local". local is true
// for the local server, whose address and data directory come from the
// same VQ_MODE, VQ_ADDR and VQ_DATA_DIR `vaquero serve` reads (config.Local;
// the server-only settings aren't read), and whose token is read from the
// data directory (empty until the server has started once).
func Resolve(name string, getenv func(string) string) (p Profile, local bool, err error) {
	cfg, err := Load()
	if err != nil {
		return p, false, err
	}
	for _, n := range []string{name, getenv("VAQUERO_PROFILE"), cfg.Current, "local"} {
		if n != "" {
			name = n
			break
		}
	}
	if name != "local" {
		p, ok := cfg.Find(name)
		if !ok {
			return p, false, fmt.Errorf("no profile called %s; add one with: vaquero profile add %s URL TOKEN", name, name)
		}
		return p, false, nil
	}
	sc, err := config.Local(getenv) // never the server-only settings
	if err != nil {
		return p, true, err
	}
	p = Profile{Name: "local", URL: "http://" + sc.Addr, DataDir: sc.DataDir}
	p.Token = LocalToken(sc.DataDir)
	return p, true, nil
}

// LocalToken reads the local server's token from its data directory.
func LocalToken(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
