package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
// standard output, which a command that ran and failed may have printed
// too.
type probe func(name string, args ...string) ([]byte, error)

// serviceProbe asks the real service manager; tests replace it, so they
// never ask about the owner's own service.
var serviceProbe probe = runProbe

// errProbeUnknown is a probe that got no answer: the command is missing,
// couldn't start, or ran past probeTimeout. A command that ran and failed
// is an answer (an *exec.ExitError), like launchctl's "not found".
var errProbeUnknown = errors.New("no answer")

// runProbe runs name with args, giving up after probeTimeout.
func runProbe(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	var ee *exec.ExitError
	if err != nil && (ctx.Err() != nil || !errors.As(err, &ee)) {
		return out, fmt.Errorf("%w: %s: %w", errProbeUnknown, name, err)
	}
	return out, err
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

// brewState is how brew services stands with a data directory.
type brewState int

const (
	// brewNone: brew services doesn't run Vaquero on it.
	brewNone brewState = iota
	// brewStopped: its service file is there, but the service manager has
	// nothing loaded (brew services stop --keep, say), so no service runs
	// the server and the CLI may.
	brewStopped
	// brewRunning: the service is loaded, so the service manager runs the
	// server and restarts it whenever it stops.
	brewRunning
)

// brewService reports whether the server on dataDir is the one brew
// services runs, and the service's label.
func brewService(dataDir string) (label string, ok bool) {
	label, state := brewServiceState(dataDir)
	return label, state == brewRunning
}

// brewServiceState is brewManaged for this machine.
func brewServiceState(dataDir string) (label string, state brewState) {
	return brewManaged(serviceOS, dataDir, os.Getenv, serviceProbe)
}

// brewManaged reports how brew services stands with dataDir, and the
// service's label. dataDir must be the default data directory, the one
// the formula's service uses (it sets no VQ_DATA_DIR): an instance with a
// data directory of its own is never the service's, so stop and upgrades
// treat it as any other.
//
// On Linux a unit file must exist (brew services run writes one too), so
// without one nothing is asked; with one, the default data directory comes
// from the systemd user manager's environment, since the shell's
// XDG_DATA_HOME may differ, and `systemctl --user is-active` says whether
// it runs. On macOS launchd is asked whether a label is loaded, with or
// without a file (brew services run loads it from a file of its own). A
// file whose service manager doesn't answer is taken at its word.
func brewManaged(goos, dataDir string, get func(string) string, ask probe) (label string, state brewState) {
	home := get("HOME")
	if home == "" {
		return "", brewNone
	}
	file := ""
	for _, l := range brewLabels(goos) {
		if _, err := os.Stat(brewServiceFile(home, goos, l)); err == nil {
			file = l
			break
		}
	}
	if goos != "darwin" && file == "" {
		return "", brewNone
	}
	env, manager := serviceEnv(goos, get, ask)
	def, err := config.DefaultDataDir(goos, env)
	if err != nil || !sameDir(dataDir, def) {
		return "", brewNone
	}
	switch {
	case goos == "darwin":
	case manager == managerAbsent:
		return file, brewStopped
	case manager == managerUnknown:
		return file, brewRunning
	}
	labels := brewLabels(goos)
	if file != "" {
		labels = append([]string{file}, slices.DeleteFunc(labels, func(l string) bool { return l == file })...)
	}
	for _, l := range labels {
		switch serviceLoaded(goos, l, ask) {
		case loadedYes:
			return l, brewRunning
		case loadedUnknown:
			if l == file {
				return l, brewRunning
			}
		}
	}
	if file != "" {
		return file, brewStopped
	}
	return "", brewNone
}

// manager is what the systemd user manager's answer to show-environment
// says about it.
type manager int

const (
	managerUp      manager = iota // it answered
	managerUnknown                // no answer, or one that can't be read
	managerAbsent                 // there is none to reach: no service runs under it
)

// busDownMessage is systemctl's complaint when there is no user manager to talk
// to (no session bus, as over a plain SSH login without lingering).
const busDownMessage = "Failed to connect to bus"

// serviceEnv is the environment the service's server starts with, as far
// as the default data directory goes. On Linux, when the systemd user
// manager answers (managerUp), its XDG_DATA_HOME and HOME (the shell's
// when it sets none); otherwise, and on macOS, the shell's. A
// show-environment that ran and said it couldn't reach the bus is
// managerAbsent; any other failure (a timeout, a missing systemctl) is
// managerUnknown. A value of XDG_DATA_HOME or HOME that can't be read (see
// unquoteSystemd) makes it unknown too: the shell's environment is used,
// never the raw text.
func serviceEnv(goos string, get func(string) string, ask probe) (env func(string) string, m manager) {
	if goos == "darwin" {
		return get, managerUnknown
	}
	out, err := ask("systemctl", "--user", "show-environment")
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && !errors.Is(err, errProbeUnknown) && bytes.Contains(ee.Stderr, []byte(busDownMessage)) {
			return get, managerAbsent
		}
		return get, managerUnknown
	}
	vals := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || (k != "XDG_DATA_HOME" && k != "HOME") {
			continue
		}
		if vals[k], ok = unquoteSystemd(v); !ok {
			return get, managerUnknown
		}
	}
	return func(k string) string {
		switch k {
		case "XDG_DATA_HOME":
			return vals[k]
		case "HOME":
			if v := vals[k]; v != "" {
				return v
			}
		}
		return get(k)
	}, managerUp
}

// unquoteSystemd reads a value as `systemctl show-environment` prints it:
// as it is, or, when it holds a space or another special character, as
// $'…' with C escapes (\\, \', \", \a \b \e \f \n \r \t \v, \xNN, and \NNN
// in octal). ok is false for anything else that is quoted, which the
// caller must not use as a path.
func unquoteSystemd(v string) (string, bool) {
	if strings.HasPrefix(v, "'") || strings.HasPrefix(v, "\"") {
		return "", false
	}
	if !strings.HasPrefix(v, "$'") {
		return v, true
	}
	body, ok := strings.CutSuffix(v[2:], "'")
	if !ok {
		return "", false
	}
	simple := map[byte]byte{'\\': '\\', '\'': '\'', '"': '"', 'a': '\a', 'b': '\b', 'e': 0x1b, 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v'}
	var out []byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '\'' {
			return "", false // an unescaped quote inside
		}
		if c != '\\' {
			out = append(out, c)
			continue
		}
		if i++; i >= len(body) {
			return "", false
		}
		c = body[i]
		switch {
		case simple[c] != 0:
			out = append(out, simple[c])
		case c == 'x':
			if i+3 > len(body) {
				return "", false
			}
			n, err := strconv.ParseUint(body[i+1:i+3], 16, 8)
			if err != nil {
				return "", false
			}
			out = append(out, byte(n))
			i += 2
		case c >= '0' && c <= '7':
			if i+3 > len(body) {
				return "", false
			}
			n, err := strconv.ParseUint(body[i:i+3], 8, 8)
			if err != nil {
				return "", false
			}
			out = append(out, byte(n))
			i += 2
		default:
			return "", false
		}
	}
	return string(out), true
}

// loaded is a service manager's answer about a label.
type loaded int

const (
	loadedNo loaded = iota
	loadedYes
	loadedUnknown // no answer (see errProbeUnknown)
)

// serviceLoaded asks the service manager about label: `launchctl print
// gui/{uid}/{label}` succeeds for a loaded agent (only the gui domain is
// asked, not user/{uid}, which Homebrew uses over SSH or sudo), and
// `systemctl --user is-active` prints active, or activating between
// keep_alive restarts.
func serviceLoaded(goos, label string, ask probe) loaded {
	if goos == "darwin" {
		_, err := ask("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
		switch {
		case err == nil:
			return loadedYes
		case errors.Is(err, errProbeUnknown):
			return loadedUnknown
		}
		return loadedNo
	}
	out, err := ask("systemctl", "--user", "is-active", label)
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "reloading", "refreshing", "deactivating":
		return loadedYes
	}
	if errors.Is(err, errProbeUnknown) {
		return loadedUnknown
	}
	return loadedNo
}
