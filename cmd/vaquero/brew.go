package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
)

// Whether brew services runs Vaquero, so the CLI leaves starting and
// stopping its server to the service manager (stop, restore, import and
// compact refusals, startLocal's wait, the stale-server restart, logs).

// serviceOS is the operating system brew services is looked up for; tests
// replace it.
var serviceOS = runtime.GOOS

// probeTimeout bounds each question to a service manager or to brew.
var probeTimeout = 2 * time.Second

// probe runs a command (launchctl, systemctl, brew) and returns its
// standard output.
type probe func(name string, args ...string) ([]byte, error)

// serviceProbe asks the real service manager; tests replace it, so they
// never ask about the owner's own service.
var serviceProbe probe = runProbe

// runProbe runs name with args, giving up after probeTimeout.
func runProbe(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}

// brewLabels are Homebrew's names for Vaquero's service, in the order brew
// services tries them (Service#plist_names and #service_names in Homebrew's
// service.rb, for a formula that names none of its own): the canonical
// sh.brew.vaquero, then the legacy homebrew.mxcl.vaquero (launchd) or
// homebrew.vaquero (systemd).
func brewLabels(goos string) []string {
	if goos == "darwin" {
		return []string{"sh.brew.vaquero", "homebrew.mxcl.vaquero"}
	}
	return []string{"sh.brew.vaquero", "homebrew.vaquero"}
}

// brewServiceFile is where brew services keeps the launchd agent or
// systemd unit called label.
func brewServiceFile(home, goos, label string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	}
	return filepath.Join(home, ".config", "systemd", "user", label+".service")
}

// brewService reports whether the server on dataDir is the one brew
// services runs, which restarts it whenever it stops, and the service's
// label.
func brewService(dataDir string) (label string, ok bool) {
	return brewManaged(serviceOS, dataDir, os.Getenv, serviceProbe)
}

// brewManaged reports whether brew services runs Vaquero on dataDir, and
// under which label. dataDir must be the default data directory, the one
// the formula's service uses (it sets no VQ_DATA_DIR): an instance with a
// data directory of its own is never the service's, so stop and upgrades
// treat it as any other. On Linux that default comes from the systemd user
// manager's environment when it answers, since the shell's XDG_DATA_HOME
// may differ. Then either a service file exists, or (after `brew services
// run`, which writes none) the service manager has the label loaded.
func brewManaged(goos, dataDir string, get func(string) string, ask probe) (label string, ok bool) {
	home := get("HOME")
	if home == "" {
		return "", false
	}
	env, managerUp := serviceEnv(goos, get, ask)
	def, err := config.DefaultDataDir(goos, env)
	if err != nil || !sameDir(dataDir, def) {
		return "", false
	}
	for _, l := range brewLabels(goos) {
		if _, err := os.Stat(brewServiceFile(home, goos, l)); err == nil {
			return l, true
		}
	}
	if goos != "darwin" && !managerUp {
		return "", false
	}
	for _, l := range brewLabels(goos) {
		if serviceLoaded(goos, l, ask) {
			return l, true
		}
	}
	return "", false
}

// serviceEnv is the environment the service's server starts with, as far
// as the default data directory goes. On Linux, when the systemd user
// manager answers (up is true), its XDG_DATA_HOME and HOME (the shell's
// when it sets none); otherwise, and on macOS, the shell's.
func serviceEnv(goos string, get func(string) string, ask probe) (env func(string) string, up bool) {
	if goos == "darwin" {
		return get, false
	}
	out, err := ask("systemctl", "--user", "show-environment")
	if err != nil {
		return get, false
	}
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			m[k] = v
		}
	}
	return func(k string) string {
		switch k {
		case "XDG_DATA_HOME":
			return m[k]
		case "HOME":
			if v, ok := m[k]; ok && v != "" {
				return v
			}
		}
		return get(k)
	}, true
}

// serviceLoaded reports whether the service manager has label loaded:
// `launchctl print gui/{uid}/{label}`, or `systemctl --user is-active`.
func serviceLoaded(goos, label string, ask probe) bool {
	if goos == "darwin" {
		_, err := ask("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
		return err == nil
	}
	_, err := ask("systemctl", "--user", "is-active", label)
	return err == nil
}
