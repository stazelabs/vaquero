package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
)

// fakeOpener replaces the browser launcher for a test and records the
// addresses it was asked to open.
func fakeOpener(t *testing.T, err error) *[]string {
	t.Helper()
	var got []string
	old := openBrowser
	openBrowser = func(url string) error { got = append(got, url); return err }
	t.Cleanup(func() { openBrowser = old })
	return &got
}

// VAQ-162: a command that starts the background server says where it is,
// once; a command that finds it running says nothing.
func TestStartLineOnceAndOnlyWhenStarted(t *testing.T) {
	_, addr := localEnv(t)
	want := "Vaquero is running at http://" + addr + ".\n"
	code, out, e := runCLI(t, "projects")
	if code != 0 || strings.Count(e, want) != 1 || strings.Contains(out, "running at") {
		t.Fatalf("%d out=%q err=%q", code, out, e)
	}
	code, _, e = runCLI(t, "projects")
	if code != 0 || strings.Contains(e, "running at") {
		t.Fatalf("second run: %d %q", code, e)
	}
}

// Under brew services the service starts the server; the CLI that waited
// for it to come up says where it is, the one that found it up doesn't.
func TestStartLineAfterWaitingForBrewService(t *testing.T) {
	dataDir, addr := brewEnv(t)
	shortBrewWait(t)
	useManager(t, &fakeManager{ok: map[string]string{launchctlPrint("sh.brew.vaquero"): "loaded\n"}})
	go func() {
		time.Sleep(200 * time.Millisecond)
		exe, _ := os.Executable()
		_, _ = client.EnsureLocal(t.Context(), exe, "http://"+addr, dataDir, 30*time.Second)
	}()
	code, _, e := runCLI(t, "projects")
	if code != 0 || strings.Count(e, "Vaquero is running at http://"+addr+".") != 1 {
		t.Fatalf("%d %q", code, e)
	}
	if code, _, e := runCLI(t, "projects"); code != 0 || strings.Contains(e, "running at") {
		t.Fatalf("second: %d %q", code, e)
	}
}

func TestOpenArgumentShapes(t *testing.T) {
	e := cliEnv(t)
	got := fakeOpener(t, nil)
	base := e.Server.URL
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"open"}, base + "/"},
		{[]string{"open", "vaq"}, base + "/p/VAQ"},
		{[]string{"open", "VAQ"}, base + "/p/VAQ"},
		{[]string{"open", "vaq-012"}, base + "/t/VAQ-12"},
	} {
		*got = nil
		code, out, errOut := cli(t, tc.args...)
		if code != 0 || len(*got) != 1 || (*got)[0] != tc.want || out != "" || errOut != "" {
			t.Errorf("%v: exit %d opened %v out %q err %q; want %s", tc.args, code, *got, out, errOut, tc.want)
		}
	}
	for _, bad := range [][]string{{"open", "not a key"}, {"open", "A", "B"}, {"open", "VAQ-0x"}} {
		*got = nil
		if code, _, _ := cli(t, bad...); code != 2 || len(*got) != 0 {
			t.Errorf("%v: exit %d, opened %v", bad, code, *got)
		}
	}
}

// When no browser can be opened the address is printed and the exit is 0.
func TestOpenPrintsAddressWhenOpenerFails(t *testing.T) {
	e := cliEnv(t)
	fakeOpener(t, errors.New("no opener"))
	code, out, errOut := cli(t, "open", "VAQ")
	if code != 0 || !strings.Contains(out, e.Server.URL+"/p/VAQ") || strings.Contains(out+errOut, "no opener") {
		t.Fatalf("%d out=%q err=%q", code, out, errOut)
	}
}

// open starts the local server like any command, and says where it is.
func TestOpenStartsTheLocalServer(t *testing.T) {
	_, addr := localEnv(t)
	got := fakeOpener(t, nil)
	code, _, e := runCLI(t, "open", "vaq-3")
	if code != 0 || len(*got) != 1 || (*got)[0] != "http://"+addr+"/t/VAQ-3" || !strings.Contains(e, "Vaquero is running at http://"+addr+".") {
		t.Fatalf("%d opened %v err %q", code, *got, e)
	}
}

// A token made with --agent acts as that agent (the request carries the
// agent, and the token is made by and for it).
func TestTokenNewWithAgent(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "token", "new", "bot", "--agent", "builder")
	if out := mustCLI(t, "token", "list"); !strings.Contains(out, "builder") {
		t.Fatalf("%s", out)
	}
}
