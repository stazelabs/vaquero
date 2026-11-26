package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeManager is a service manager for tests: it answers the commands in
// ok with their output and fails every other, recording each call.
type fakeManager struct {
	mu    sync.Mutex
	ok    map[string]string
	calls []string
}

func (f *fakeManager) probe(name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, line)
	if out, ok := f.ok[line]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("exit status 1")
}

func (f *fakeManager) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// useManager makes the CLI's brew services checks ask m.
func useManager(t *testing.T, m *fakeManager) {
	t.Helper()
	old := serviceProbe
	serviceProbe = m.probe
	t.Cleanup(func() { serviceProbe = old })
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func touchFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

var launchdDomain = "gui/" + strconv.Itoa(os.Getuid())

// On Linux the service's default data directory is the one the systemd user
// manager's environment gives, which can differ from the shell's.
func TestBrewManagedLinuxUsesTheManagersXDG(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "linux", "sh.brew.vaquero"))
	shellXDG := filepath.Join(home, "shell-xdg")
	plain := filepath.Join(home, ".local", "share", "vaquero")
	shell := envOf(map[string]string{"HOME": home, "XDG_DATA_HOME": shellXDG})

	// The manager sets no XDG_DATA_HOME: the service uses ~/.local/share.
	m := &fakeManager{ok: map[string]string{"systemctl --user show-environment": "HOME=" + home + "\nPATH=/usr/bin\n"}}
	if _, ok := brewManaged("linux", filepath.Join(shellXDG, "vaquero"), shell, m.probe); ok {
		t.Error("the shell's XDG directory taken for the service's")
	}
	if _, ok := brewManaged("linux", plain, shell, m.probe); !ok {
		t.Error("the manager's default directory not taken for the service's")
	}

	// The manager has an XDG_DATA_HOME of its own.
	mgrXDG := filepath.Join(home, "manager-xdg")
	m = &fakeManager{ok: map[string]string{"systemctl --user show-environment": "XDG_DATA_HOME=" + mgrXDG + "\n"}}
	if _, ok := brewManaged("linux", filepath.Join(mgrXDG, "vaquero"), shell, m.probe); !ok {
		t.Error("the manager's XDG directory not taken for the service's")
	}
	if _, ok := brewManaged("linux", filepath.Join(shellXDG, "vaquero"), shell, m.probe); ok {
		t.Error("the shell's XDG directory taken for the service's")
	}

	// No manager to ask: the shell's environment, as before.
	none := &fakeManager{}
	if _, ok := brewManaged("linux", filepath.Join(shellXDG, "vaquero"), shell, none.probe); !ok {
		t.Error("without a manager, the shell's default directory isn't the service's")
	}
}

// macOS has no XDG question: launchd is never asked for its environment.
func TestBrewManagedMacAsksNothingWithAServiceFile(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "darwin", "sh.brew.vaquero"))
	m := &fakeManager{}
	if _, ok := brewManaged("darwin", filepath.Join(home, "Library", "Application Support", "vaquero"), envOf(map[string]string{"HOME": home}), m.probe); !ok {
		t.Fatal("not managed")
	}
	if c := m.called(); len(c) != 0 {
		t.Fatalf("asked %v", c)
	}
}

// `brew services run` starts the service without a file: the service
// manager reporting the label loaded counts too, for the default data
// directory only.
func TestBrewManagedServiceRunWithoutAFile(t *testing.T) {
	home := t.TempDir()
	get := envOf(map[string]string{"HOME": home})
	mac := filepath.Join(home, "Library", "Application Support", "vaquero")
	linux := filepath.Join(home, ".local", "share", "vaquero")
	for _, c := range []struct {
		goos, dir, cmd, label string
	}{
		{"darwin", mac, "launchctl print " + launchdDomain + "/sh.brew.vaquero", "sh.brew.vaquero"},
		{"darwin", mac, "launchctl print " + launchdDomain + "/homebrew.mxcl.vaquero", "homebrew.mxcl.vaquero"},
		{"linux", linux, "systemctl --user is-active sh.brew.vaquero", "sh.brew.vaquero"},
		{"linux", linux, "systemctl --user is-active homebrew.vaquero", "homebrew.vaquero"},
	} {
		m := &fakeManager{ok: map[string]string{c.cmd: "loaded\n", "systemctl --user show-environment": "HOME=" + home + "\n"}}
		if label, ok := brewManaged(c.goos, c.dir, get, m.probe); !ok || label != c.label {
			t.Errorf("%s: got %q %v, want %q", c.cmd, label, ok, c.label)
		}
		// Another data directory is never the service's, and the service
		// manager isn't asked about one.
		m = &fakeManager{ok: map[string]string{c.cmd: "loaded\n"}}
		if _, ok := brewManaged(c.goos, filepath.Join(home, "other"), get, m.probe); ok {
			t.Errorf("%s: a custom data directory taken for the service's", c.cmd)
		}
		for _, call := range m.called() {
			if !strings.HasSuffix(call, "show-environment") {
				t.Errorf("%s: asked %q about a custom data directory", c.cmd, call)
			}
		}
	}
	// Nothing loaded, no file: not managed.
	if _, ok := brewManaged("darwin", mac, get, (&fakeManager{}).probe); ok {
		t.Error("managed with nothing loaded")
	}
}

// Each question to the service manager gives up after probeTimeout.
func TestServiceProbeIsBounded(t *testing.T) {
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	start := time.Now()
	if _, err := runProbe("sleep", "5"); err == nil {
		t.Fatal("no error from a probe that timed out")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %v", took)
	}
	if probeTimeout != 200*time.Millisecond || old != 2*time.Second {
		t.Fatalf("the bound is %v", old)
	}
}

// Under `brew services run` (no file), stop still leaves the server to brew.
func TestStopUnderBrewServicesRun(t *testing.T) {
	brewEnvNamed(t, "")
	useManager(t, &fakeManager{ok: map[string]string{"launchctl print " + launchdDomain + "/sh.brew.vaquero": "loaded\n"}})
	code, _, e := runCLI(t, "stop")
	if code != 1 || !strings.Contains(e, "Vaquero runs under brew services (sh.brew.vaquero)") {
		t.Fatalf("%d %q", code, e)
	}
}
