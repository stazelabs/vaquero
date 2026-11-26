package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/version"
)

// fakeManager is a service manager for tests: it answers the commands in
// ok with their output, the commands in fail with their output and an
// error (as systemctl is-active prints "inactive" and exits 3), and fails
// every other, recording each call. With unknown, those other failures are
// errProbeUnknown (no answer at all, as on a timeout).
type fakeManager struct {
	mu      sync.Mutex
	ok      map[string]string
	fail    map[string]string
	unknown bool
	calls   []string
}

func (f *fakeManager) probe(name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, line)
	if out, ok := f.ok[line]; ok {
		return []byte(out), nil
	}
	if out, ok := f.fail[line]; ok {
		return []byte(out), errors.New("exit status 3")
	}
	if f.unknown {
		return nil, errProbeUnknown
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

// everythingLoaded is a service manager with every label loaded and no
// systemd user manager to ask for its environment.
func everythingLoaded(name string, args ...string) ([]byte, error) {
	if name == "launchctl" || slices.Contains(args, "is-active") {
		return []byte("active\n"), nil
	}
	return nil, errors.New("exit status 1")
}

// brewRunningOn is brewManaged reduced to "the service runs it".
func brewRunningOn(goos, dataDir string, get func(string) string, ask probe) (string, bool) {
	label, state := brewManaged(goos, dataDir, get, ask)
	return label, state == brewRunning
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

func launchctlPrint(label string) string { return "launchctl print " + launchdDomain + "/" + label }

func isActive(label string) string { return "systemctl --user is-active " + label }

const showEnv = "systemctl --user show-environment"

// On Linux the service's default data directory is the one the systemd user
// manager's environment gives, which can differ from the shell's.
func TestBrewManagedLinuxUsesTheManagersXDG(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "linux", "sh.brew.vaquero"))
	shellXDG := filepath.Join(home, "shell-xdg")
	plain := filepath.Join(home, ".local", "share", "vaquero")
	shell := envOf(map[string]string{"HOME": home, "XDG_DATA_HOME": shellXDG})
	active := isActive("sh.brew.vaquero")

	// The manager sets no XDG_DATA_HOME: the service uses ~/.local/share.
	m := &fakeManager{ok: map[string]string{showEnv: "HOME=" + home + "\nPATH=/usr/bin\n", active: "active\n"}}
	if _, ok := brewRunningOn("linux", filepath.Join(shellXDG, "vaquero"), shell, m.probe); ok {
		t.Error("the shell's XDG directory taken for the service's")
	}
	if _, ok := brewRunningOn("linux", plain, shell, m.probe); !ok {
		t.Error("the manager's default directory not taken for the service's")
	}

	// The manager has an XDG_DATA_HOME of its own.
	mgrXDG := filepath.Join(home, "manager-xdg")
	m = &fakeManager{ok: map[string]string{showEnv: "XDG_DATA_HOME=" + mgrXDG + "\n", active: "active\n"}}
	if _, ok := brewRunningOn("linux", filepath.Join(mgrXDG, "vaquero"), shell, m.probe); !ok {
		t.Error("the manager's XDG directory not taken for the service's")
	}
	if _, ok := brewRunningOn("linux", filepath.Join(shellXDG, "vaquero"), shell, m.probe); ok {
		t.Error("the shell's XDG directory taken for the service's")
	}

	// No manager to ask: the shell's environment, and the unit file alone,
	// as before.
	none := &fakeManager{}
	if _, ok := brewRunningOn("linux", filepath.Join(shellXDG, "vaquero"), shell, none.probe); !ok {
		t.Error("without a manager, the shell's default directory isn't the service's")
	}
}

// systemd quotes a value with a space (or another special character) as
// $'…', with C escapes; the path is read unquoted, never raw.
func TestBrewManagedLinuxReadsQuotedXDG(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "linux", "sh.brew.vaquero"))
	shell := envOf(map[string]string{"HOME": home})
	active := isActive("sh.brew.vaquero")
	spaced := filepath.Join(home, "my data")
	m := &fakeManager{ok: map[string]string{showEnv: "XDG_DATA_HOME=$'" + spaced + "'\n", active: "active\n"}}
	if _, ok := brewRunningOn("linux", filepath.Join(spaced, "vaquero"), shell, m.probe); !ok {
		t.Error("a quoted XDG_DATA_HOME with a space wasn't read")
	}
	// A value that can't be read: the manager counts as unknown, and the
	// shell's environment is used, not the raw text.
	m = &fakeManager{ok: map[string]string{showEnv: "XDG_DATA_HOME=$'" + spaced + "\\q'\n", active: "active\n"}}
	if _, ok := brewRunningOn("linux", filepath.Join(home, ".local", "share", "vaquero"), shell, m.probe); !ok {
		t.Error("an unreadable value wasn't left for the shell's environment")
	}
}

func TestUnquoteSystemd(t *testing.T) {
	for in, want := range map[string]string{
		"/plain/path":           "/plain/path",
		"$'/a b'":               "/a b",
		`$'/it\'s'`:             "/it's",
		`$'/back\\slash'`:       `/back\slash`,
		`$'/tab\there'`:         "/tab\there",
		`$'/hex\x41\x42'`:       "/hexAB",
		`$'/nl\n\"q\"'`:         "/nl\n\"q\"",
		`$'/oct\101'`:           "/octA",
		`$'/caf\xc3\xa9 notes'`: "/café notes",
	} {
		got, ok := unquoteSystemd(in)
		if !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{`$'/open`, `$'/bad\q'`, `$'/short\x4'`, `'/single'`, `"/double"`, `$'/a'b'`, `$'/trail\'`} {
		if got, ok := unquoteSystemd(in); ok {
			t.Errorf("%q read as %q", in, got)
		}
	}
}

// On Linux without a unit file nothing is asked: brew services run writes
// the unit too, so no file means no service, and no command forks a
// systemctl when brew isn't in use.
func TestBrewManagedLinuxAsksNothingWithoutAUnit(t *testing.T) {
	home := t.TempDir()
	m := &fakeManager{ok: map[string]string{showEnv: "HOME=" + home + "\n", isActive("sh.brew.vaquero"): "active\n"}}
	if _, state := brewManaged("linux", filepath.Join(home, ".local", "share", "vaquero"), envOf(map[string]string{"HOME": home}), m.probe); state != brewNone {
		t.Fatalf("state %v", state)
	}
	if c := m.called(); len(c) != 0 {
		t.Fatalf("asked %v", c)
	}
}

// macOS asks launchd only whether the label is loaded, never for an
// environment.
func TestBrewManagedMacAsksOnlyLaunchd(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "darwin", "sh.brew.vaquero"))
	m := &fakeManager{ok: map[string]string{launchctlPrint("sh.brew.vaquero"): "loaded\n"}}
	if _, ok := brewRunningOn("darwin", filepath.Join(home, "Library", "Application Support", "vaquero"), envOf(map[string]string{"HOME": home}), m.probe); !ok {
		t.Fatal("not managed")
	}
	if c := m.called(); !slices.Equal(c, []string{launchctlPrint("sh.brew.vaquero")}) {
		t.Fatalf("asked %v", c)
	}
}

// A service file with nothing loaded (brew services stop --keep, or a
// crashed manager) is brewStopped: the service isn't running the server.
// When the manager can't be asked at all, the file is taken at its word.
func TestBrewManagedFileWithNothingLoaded(t *testing.T) {
	home := t.TempDir()
	get := envOf(map[string]string{"HOME": home})
	mac := filepath.Join(home, "Library", "Application Support", "vaquero")
	linux := filepath.Join(home, ".local", "share", "vaquero")
	touchFile(t, brewServiceFile(home, "darwin", "sh.brew.vaquero"))
	touchFile(t, brewServiceFile(home, "linux", "homebrew.vaquero"))
	if label, state := brewManaged("darwin", mac, get, (&fakeManager{}).probe); state != brewStopped || label != "sh.brew.vaquero" {
		t.Errorf("darwin, nothing loaded: %q %v", label, state)
	}
	if _, state := brewManaged("darwin", mac, get, (&fakeManager{unknown: true}).probe); state != brewRunning {
		t.Errorf("darwin, launchd not answering: %v", state)
	}
	inactive := &fakeManager{ok: map[string]string{showEnv: "HOME=" + home + "\n"}, fail: map[string]string{isActive("homebrew.vaquero"): "inactive\n"}}
	if label, state := brewManaged("linux", linux, get, inactive.probe); state != brewStopped || label != "homebrew.vaquero" {
		t.Errorf("linux, inactive: %q %v", label, state)
	}
	// systemd between keep_alive restarts says "activating" and exits 3:
	// still the service's.
	restarting := &fakeManager{ok: map[string]string{showEnv: "HOME=" + home + "\n"}, fail: map[string]string{isActive("homebrew.vaquero"): "activating\n"}}
	if _, state := brewManaged("linux", linux, get, restarting.probe); state != brewRunning {
		t.Errorf("linux, activating: %v", state)
	}
}

// `brew services run` on macOS starts the service without a file: launchd
// reporting the label loaded counts too, for the default data directory
// only.
func TestBrewManagedServiceRunWithoutAFile(t *testing.T) {
	home := t.TempDir()
	get := envOf(map[string]string{"HOME": home})
	mac := filepath.Join(home, "Library", "Application Support", "vaquero")
	for _, label := range []string{"sh.brew.vaquero", "homebrew.mxcl.vaquero"} {
		m := &fakeManager{ok: map[string]string{launchctlPrint(label): "loaded\n"}}
		if got, ok := brewRunningOn("darwin", mac, get, m.probe); !ok || got != label {
			t.Errorf("%s: got %q %v", label, got, ok)
		}
		// Another data directory is never the service's, and launchd isn't
		// asked about one.
		m = &fakeManager{ok: map[string]string{launchctlPrint(label): "loaded\n"}}
		if _, ok := brewRunningOn("darwin", filepath.Join(home, "other"), get, m.probe); ok {
			t.Errorf("%s: a custom data directory taken for the service's", label)
		}
		if c := m.called(); len(c) != 0 {
			t.Errorf("%s: asked %v about a custom data directory", label, c)
		}
	}
	// Nothing loaded, no file: not managed.
	if _, state := brewManaged("darwin", mac, get, (&fakeManager{}).probe); state != brewNone {
		t.Errorf("state %v with nothing loaded", state)
	}
}

// Each question to the service manager gives up after probeTimeout, and an
// unanswered one is errProbeUnknown; a command that ran and failed isn't.
func TestServiceProbeIsBounded(t *testing.T) {
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	start := time.Now()
	if _, err := runProbe("sleep", "5"); !errors.Is(err, errProbeUnknown) {
		t.Fatalf("a probe that timed out: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %v", took)
	}
	if old != 2*time.Second {
		t.Fatalf("the bound is %v", old)
	}
	if _, err := runProbe("false"); err == nil || errors.Is(err, errProbeUnknown) {
		t.Fatalf("a probe that failed: %v", err)
	}
	if _, err := runProbe(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, errProbeUnknown) {
		t.Fatalf("a missing command: %v", err)
	}
	var ee *exec.ExitError
	if _, err := runProbe("false"); !errors.As(err, &ee) {
		t.Fatalf("the exit error is kept: %v", err)
	}
}

// Under `brew services run` (no file), stop still leaves the server to brew.
func TestStopUnderBrewServicesRun(t *testing.T) {
	brewEnvNamed(t, "")
	useManager(t, &fakeManager{ok: map[string]string{launchctlPrint("sh.brew.vaquero"): "loaded\n"}})
	code, _, e := runCLI(t, "stop")
	if code != 1 || !strings.Contains(e, "Vaquero runs under brew services (sh.brew.vaquero)") {
		t.Fatalf("%d %q", code, e)
	}
}

// With the service file there but nothing loaded (brew services stop
// --keep), stop stops the server like any other.
func TestStopWithTheServiceNotLoaded(t *testing.T) {
	dataDir, _ := brewEnv(t)
	serverAt(t, dataDir, "0.1.0")
	useManager(t, &fakeManager{})
	code, out, e := runCLI(t, "stop")
	if code != 0 || strings.TrimSpace(out) != "Vaquero stopped." {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if _, running, _ := lockfile.Holder(dataDir); running {
		t.Fatal("server still holds the lock")
	}
}

// shortBrewWait makes the wait for the service manager short.
func shortBrewWait(t *testing.T) {
	t.Helper()
	old := brewRestartWait
	brewRestartWait = 500 * time.Millisecond
	t.Cleanup(func() { brewRestartWait = old })
}

// Under brew services, when the service doesn't bring the server back, the
// command says so and starts nothing: a server started here would hold
// the port and the data directory, and the service's own would fail.
func TestBrewServiceThatNeverComesBack(t *testing.T) {
	dataDir, _ := brewEnv(t)
	shortBrewWait(t)
	code, _, e := runCLI(t, "projects")
	if code != 1 || strings.TrimSpace(e) != "The brew service didn't come back; run brew services restart vaquero." {
		t.Fatalf("%d %q", code, e)
	}
	if _, running, _ := lockfile.Holder(dataDir); running {
		t.Fatal("the command started a server")
	}
}

// With the service file there but nothing loaded, the command starts a
// server itself at once, and says once how to hand it back to brew
// services.
func TestBrewServiceNotLoadedStartsOutsideIt(t *testing.T) {
	dataDir, _ := brewEnv(t)
	useManager(t, &fakeManager{})
	start := time.Now()
	code, _, e := runCLI(t, "projects")
	if code != 0 {
		t.Fatalf("%d %q", code, e)
	}
	if took := time.Since(start); took >= brewRestartWait {
		t.Fatalf("waited %v for brew services", took)
	}
	if _, running, _ := lockfile.Holder(dataDir); !running {
		t.Fatal("no server")
	}
	note := "vaquero: the brew service (sh.brew.vaquero) isn't loaded, so this server runs outside brew services; to hand it back, run vaquero stop, then brew services start vaquero"
	if strings.Count(e, note) != 1 {
		t.Fatalf("stderr %q", e)
	}
	// Running already: no note.
	if code, _, e := runCLI(t, "projects"); code != 0 || strings.Contains(e, "outside brew services") {
		t.Fatalf("%d %q", code, e)
	}
}

// A server that already answers needs no question to the service manager:
// startLocal returns before any probe. (After replaceStale's stop nothing
// answers, so the stale-restart path still probes: TestStaleRestartUnderBrewServices.)
func TestRunningServerAsksNoServiceManager(t *testing.T) {
	dataDir, _ := brewEnv(t)
	serverAt(t, dataDir, version.Version)
	m := &fakeManager{ok: map[string]string{launchctlPrint("sh.brew.vaquero"): "loaded\n"}}
	useManager(t, m)
	if code, _, e := runCLI(t, "projects"); code != 0 {
		t.Fatalf("%d %q", code, e)
	}
	if c := m.called(); len(c) != 0 {
		t.Fatalf("asked %v", c)
	}
}

// busDown is systemctl --user failing as it does with no user manager to
// talk to: a real exit, with the reason on standard error.
func busDown(string, ...string) ([]byte, error) {
	return exec.Command("sh", "-c", "echo 'Failed to connect to bus: No medium found' >&2; exit 1").Output()
}

// On Linux with a unit file, a user manager that can't be reached at all
// runs no service: brewStopped. A manager that doesn't answer in time, or a
// missing systemctl, is unknown, which takes the file at its word.
func TestBrewManagedLinuxBusDown(t *testing.T) {
	home := t.TempDir()
	touchFile(t, brewServiceFile(home, "linux", "sh.brew.vaquero"))
	get := envOf(map[string]string{"HOME": home})
	linux := filepath.Join(home, ".local", "share", "vaquero")
	if label, state := brewManaged("linux", linux, get, busDown); state != brewStopped || label != "sh.brew.vaquero" {
		t.Errorf("bus down: %q %v", label, state)
	}
	if _, state := brewManaged("linux", linux, get, (&fakeManager{unknown: true}).probe); state != brewRunning {
		t.Errorf("no answer: %v", state)
	}
	other := func(string, ...string) ([]byte, error) {
		return exec.Command("sh", "-c", "echo 'Something else went wrong' >&2; exit 1").Output()
	}
	if _, state := brewManaged("linux", linux, get, other); state != brewRunning {
		t.Errorf("another failure: %v", state)
	}
}
