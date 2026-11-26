package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/lockfile"
)

// writeLog writes lines numbered from 1 to n to path, each "line N".
func writeLog(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendLog(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// VAQ-58: logs prints the last 100 lines of {data dir}/vaquero.log, or -n
// of them, and starts no server.
func TestLogsPrintsTheLastLines(t *testing.T) {
	dataDir, _ := localEnv(t)
	writeLog(t, filepath.Join(dataDir, "vaquero.log"), 150)
	code, out, e := runCLI(t, "logs")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 100 || lines[0] != "line 51" || lines[99] != "line 150" {
		t.Fatalf("%d %q: %d lines, %q to %q", code, e, len(lines), lines[0], lines[len(lines)-1])
	}
	if code, out, _ := runCLI(t, "logs", "-n", "3"); code != 0 || out != "line 148\nline 149\nline 150\n" {
		t.Fatalf("-n 3: %d %q", code, out)
	}
	if code, out, _ := runCLI(t, "logs", "-n", "500"); code != 0 || strings.Count(out, "\n") != 150 {
		t.Fatalf("-n 500: %d, %d lines", code, strings.Count(out, "\n"))
	}
	if code, out, _ := runCLI(t, "logs", "-n", "0"); code != 0 || out != "" {
		t.Fatalf("-n 0: %d %q", code, out)
	}
	if code, _, e := runCLI(t, "logs", "-n", "-1"); code != 2 || !strings.Contains(e, "usage: vaquero logs") {
		t.Fatalf("-n -1: %d %q", code, e)
	}
	appendLog(t, filepath.Join(dataDir, "vaquero.log"), "no newline yet")
	if code, out, _ := runCLI(t, "logs", "-n", "2"); code != 0 || out != "line 150\nno newline yet\n" {
		t.Fatalf("a last line without a newline: %d %q", code, out)
	}
	if _, running, _ := lockfile.Holder(dataDir); running {
		t.Fatal("logs started a server")
	}
}

// The last lines are found across many reads from the end of a big log.
func TestTailLinesAcrossChunks(t *testing.T) {
	old := tailChunk
	tailChunk = 7
	t.Cleanup(func() { tailChunk = old })
	path := filepath.Join(t.TempDir(), "vaquero.log")
	writeLog(t, path, 40)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	b, err := tailLines(f, st.Size(), 12)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 12 || lines[0] != "line 29" || lines[11] != "line 40" {
		t.Fatalf("%q", b)
	}
}

func TestLogsWithoutALog(t *testing.T) {
	dataDir, _ := localEnv(t)
	code, _, e := runCLI(t, "logs")
	if want := "No log at " + filepath.Join(dataDir, "vaquero.log") + " yet."; code != 1 || strings.TrimSpace(e) != want {
		t.Fatalf("%d %q", code, e)
	}
}

func TestLogsPath(t *testing.T) {
	dataDir, _ := localEnv(t)
	code, out, _ := runCLI(t, "logs", "--path")
	if code != 0 || out != filepath.Join(dataDir, "vaquero.log")+"\n" {
		t.Fatalf("%d %q", code, out)
	}
}

// useExecutable makes the CLI's own path exe.
func useExecutable(t *testing.T, exe string) {
	t.Helper()
	old := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = old })
}

// VAQ-121: under brew services the log is the formula's log_path,
// {brew prefix}/var/log/vaquero.log; the prefix comes from the running
// binary's path, else from brew --prefix.
func TestLogsUnderBrewServices(t *testing.T) {
	brewEnv(t)
	prefix, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(prefix, "var", "log", "vaquero.log")

	exe := filepath.Join(prefix, "Cellar", "vaquero", "0.1.3", "bin", "vaquero")
	touchFile(t, exe)
	link := filepath.Join(prefix, "bin", "vaquero")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/vaquero/0.1.3/bin/vaquero", link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{exe, link, filepath.Join(prefix, "opt", "vaquero", "bin", "vaquero")} {
		useExecutable(t, p)
		if code, out, e := runCLI(t, "logs", "--path"); code != 0 || out != want+"\n" {
			t.Errorf("%s: %d %q %q", p, code, out, e)
		}
	}

	// Not a Homebrew binary: ask brew.
	useExecutable(t, filepath.Join(t.TempDir(), "vaquero"))
	m := &fakeManager{ok: map[string]string{"brew --prefix": prefix + "\n"}}
	useManager(t, m)
	if code, out, e := runCLI(t, "logs", "--path"); code != 0 || out != want+"\n" {
		t.Errorf("brew --prefix: %d %q %q", code, out, e)
	}
	writeLog(t, want, 5)
	if code, out, e := runCLI(t, "logs", "-n", "1"); code != 0 || out != "line 5\n" {
		t.Errorf("logs: %d %q %q", code, out, e)
	}

	// And brew can't be asked.
	useManager(t, &fakeManager{})
	if code, _, e := runCLI(t, "logs"); code != 1 || !strings.Contains(e, "Homebrew's prefix") {
		t.Errorf("no prefix: %d %q", code, e)
	}
}

// An instance with its own data directory reads its own log, even when
// brew services runs Vaquero too.
func TestLogsWithOwnDataDirUnderBrewServices(t *testing.T) {
	dataDir, _ := localEnv(t)
	brewServiceFileExists(t)
	if code, out, _ := runCLI(t, "logs", "--path"); code != 0 || out != filepath.Join(dataDir, "vaquero.log")+"\n" {
		t.Fatalf("%d %q", code, out)
	}
}

// syncBuffer is a bytes.Buffer safe for a writer and a reader at once.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitFor waits until out holds want.
func waitFor(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %q; have %q", want, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// -f follows the log as it grows and across a rotation, losing no line:
// the rest of the old file, then the new one from its start.
func TestLogsFollowAcrossRotation(t *testing.T) {
	dataDir, _ := localEnv(t)
	old := followPoll
	followPoll = 20 * time.Millisecond
	t.Cleanup(func() { followPoll = old })
	path := filepath.Join(dataDir, "vaquero.log")
	writeLog(t, path, 3)

	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut syncBuffer
	done := make(chan int)
	go func() { done <- cmdLogs(ctx, &out, &errOut, []string{"-f", "-n", "2"}) }()
	waitFor(t, &out, "line 2\nline 3\n")
	appendLog(t, path, "line 4\n")
	waitFor(t, &out, "line 4\n")

	// The writer's rotation: the file moves to .1, and a line written to
	// it just before the new file starts is still shown.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendLog(t, path+".1", "line 5\n")
	appendLog(t, path, "line 6\n")
	waitFor(t, &out, "line 6\n")
	appendLog(t, path, "line 7\n")
	waitFor(t, &out, "line 7\n")
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d: %q", code, errOut.String())
	}
	if got := out.String(); got != "line 2\nline 3\nline 4\nline 5\nline 6\nline 7\n" {
		t.Fatalf("%q", got)
	}
}

// -f waits for a log that isn't there yet.
func TestLogsFollowWaitsForTheLog(t *testing.T) {
	dataDir, _ := localEnv(t)
	old := followPoll
	followPoll = 20 * time.Millisecond
	t.Cleanup(func() { followPoll = old })
	path := filepath.Join(dataDir, "vaquero.log")
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut syncBuffer
	done := make(chan int)
	go func() { done <- cmdLogs(ctx, &out, &errOut, []string{"-f"}) }()
	time.Sleep(60 * time.Millisecond)
	writeLog(t, path, 2)
	waitFor(t, &out, "line 1\nline 2\n")
	cancel()
	if code := <-done; code != 0 || out.String() != "line 1\nline 2\n" {
		t.Fatalf("exit %d: %q %q", code, out.String(), errOut.String())
	}
}
