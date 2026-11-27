package client_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/logfile"
)

// TestMain doubles as a fake `vaquero serve` for TestEnsureLocalStarts:
// with VQ_FAKE_SERVE set it answers /healthz on VQ_ADDR for a few seconds.
func TestMain(m *testing.M) {
	if os.Getenv("VQ_FAKE_SERVE") == "1" {
		ln, err := net.Listen("tcp", os.Getenv("VQ_ADDR"))
		if err != nil {
			os.Exit(1)
		}
		go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDoDecodesAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Vaquero-Agent") != "builder" ||
			r.Header.Get("Vaquero-Client") != "vaquero-cli/1.2.0" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Vaquero-Min-Client", "9.0.0")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/ok" {
			fmt.Fprint(w, `{"key":"VAQ-1"}`)
			return
		}
		w.WriteHeader(409)
		fmt.Fprint(w, `{"error":{"code":"conflict","message":"VAQ-1 changed since you read it.","current":{"version":3}}}`)
	}))
	defer srv.Close()
	c := &client.Client{BaseURL: srv.URL, Token: "tok", Agent: "builder", Version: "1.2.0"}
	var out struct{ Key string }
	if err := c.Do(context.Background(), "GET", "/api/v1/ok", nil, &out); err != nil || out.Key != "VAQ-1" {
		t.Fatalf("%v %+v", err, out)
	}
	if min, need := c.UpgradeNeeded(); !need || min != "9.0.0" {
		t.Fatalf("%s %v", min, need)
	}
	err := c.Do(context.Background(), "PATCH", "/api/v1/conflict", map[string]int{"version": 2}, nil)
	var e *client.Error
	if !errors.As(err, &e) || e.Status != 409 || e.Code != "conflict" || !strings.Contains(string(e.Current), `"version":3`) {
		t.Fatalf("%v", err)
	}
	if e.Error() != "VAQ-1 changed since you read it." {
		t.Fatal(e.Error())
	}
}

func TestProfilesRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfg, err := client.Load()
	if err != nil || len(cfg.Profiles) != 0 {
		t.Fatalf("%+v %v", cfg, err)
	}
	cfg.Profiles = append(cfg.Profiles, client.Profile{Name: "team", URL: "https://vq.example", Token: "vq_x"})
	cfg.Current = "team"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	path, _ := client.ConfigPath()
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	back, _ := client.Load()
	if back.Current != "team" || len(back.Profiles) != 1 || back.Profiles[0].Token != "vq_x" {
		t.Fatalf("%+v", back)
	}
	env := map[string]string{"VQ_DATA_DIR": t.TempDir()}
	get := func(k string) string { return env[k] }
	if p, local, err := client.Resolve("", get); err != nil || local || p.Name != "team" {
		t.Fatalf("current: %+v %v %v", p, local, err)
	}
	env["VAQUERO_PROFILE"] = "local"
	if p, local, err := client.Resolve("", get); err != nil || !local || p.Name != "local" {
		t.Fatalf("env: %+v %v %v", p, local, err)
	}
	env["VAQUERO_SERVER"] = "team"
	if p, local, err := client.Resolve("", get); err != nil || local || p.Name != "team" {
		t.Fatalf("VAQUERO_SERVER wins over VAQUERO_PROFILE: %+v %v %v", p, local, err)
	}
	delete(env, "VAQUERO_SERVER")
	if p, _, err := client.Resolve("team", get); err != nil || p.URL != "https://vq.example" {
		t.Fatalf("flag: %+v %v", p, err)
	}
	if _, _, err := client.Resolve("nope", get); err == nil || !strings.Contains(err.Error(), "no server called nope") {
		t.Fatalf("%v", err)
	}
}

// R11: settings only the server reads (log level and format, attachment
// storage) never stop the command line from finding the local server.
func TestResolveLocalIgnoresServerOnlySettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	env := map[string]string{
		"VQ_DATA_DIR": t.TempDir(), "VQ_ADDR": "127.0.0.1:9999",
		"VQ_LOG_LEVEL": "verbose", "VQ_LOG_FORMAT": "xml", "VQ_BLOB_STORE": "s3",
	}
	get := func(k string) string { return env[k] }
	p, local, err := client.Resolve("local", get)
	if err != nil || !local || p.URL != "http://127.0.0.1:9999" {
		t.Fatalf("%+v %v %v", p, local, err)
	}
	env["VQ_ADDR"] = "0.0.0.0:9999"
	if _, _, err := client.Resolve("local", get); err == nil || !strings.Contains(err.Error(), "VQ_ADDR") {
		t.Fatalf("a network address: %v", err)
	}
}

func TestResolveLocalReadsTokenFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "token"), []byte("vq_abc\n"), 0o600)
	env := map[string]string{"VQ_DATA_DIR": dir, "VQ_ADDR": "127.0.0.1:9999"}
	p, local, err := client.Resolve("local", func(k string) string { return env[k] })
	if err != nil || !local || p.URL != "http://127.0.0.1:9999" || p.Token != "vq_abc" || p.DataDir != dir {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestEnsureLocalAlreadyUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer srv.Close()
	if err := client.EnsureLocal(context.Background(), "/no/such/exe", srv.URL, t.TempDir(), time.Second); err != nil {
		t.Fatal(err)
	}
}

// Review Focus 5.
func TestEnsureLocalPortTakenByOther(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // hold it open, never answer
		}
	}()
	start := time.Now()
	err = client.EnsureLocal(context.Background(), "/no/such/exe", "http://"+ln.Addr().String(), t.TempDir(), 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), ln.Addr().String()) {
		t.Fatalf("%v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestEnsureLocalStarts(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	t.Setenv("VQ_FAKE_SERVE", "1")
	exe, _ := os.Executable()
	dir := t.TempDir()
	// VAQ-61: a log at the limit moves to vaquero.log.1 before the start.
	full := bytes.Repeat([]byte("x"), logfile.Max)
	if err := os.WriteFile(filepath.Join(dir, "vaquero.log"), full, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureLocal(context.Background(), exe, "http://"+addr, dir, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dir, "vaquero.log")); err != nil || st.Size() >= logfile.Max {
		t.Fatalf("log not rotated: %v", err)
	}
	if st, err := os.Stat(filepath.Join(dir, "vaquero.log.1")); err != nil || st.Size() != logfile.Max {
		t.Fatalf("no vaquero.log.1: %v", err)
	}
}

func TestEnsureLocalReportsFailedStart(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	dir := t.TempDir()
	err := client.EnsureLocal(context.Background(), "/usr/bin/false", "http://"+addr, dir, 3*time.Second)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "vaquero.log")) {
		t.Fatalf("%v", err)
	}
}

// A field message the main message already says isn't repeated.
func TestErrorMessageNotRepeated(t *testing.T) {
	e := &client.Error{Message: "VAQ-1 already blocks this ticket.", Fields: map[string]string{"to": "VAQ-1 already blocks this ticket."}}
	if e.Error() != "VAQ-1 already blocks this ticket." {
		t.Fatalf("%q", e.Error())
	}
	e = &client.Error{Message: "Some names don't match anything.", Fields: map[string]string{"status": "No status Nope in VAQ."}}
	if e.Error() != "Some names don't match anything. (status: No status Nope in VAQ.)" {
		t.Fatalf("%q", e.Error())
	}
}

func TestUpload(t *testing.T) {
	var gotName, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, h, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		b := make([]byte, 100)
		n, _ := f.Read(b)
		gotName, gotBody, gotAuth = h.Filename, string(b[:n]), r.Header.Get("Authorization")
		if gotName == "refuse.txt" {
			w.WriteHeader(413)
			fmt.Fprint(w, `{"error":{"code":"too_large","message":"Files can be at most 10 MB."}}`)
			return
		}
		fmt.Fprint(w, `{"id":"A1"}`)
	}))
	defer srv.Close()
	c := &client.Client{BaseURL: srv.URL, Token: "vq_t"}
	var out struct{ ID string }
	if err := c.Upload(t.Context(), "/up", `a "quoted"\name.txt`, strings.NewReader("hello"), &out); err != nil ||
		out.ID != "A1" || gotName != `a "quoted"\name.txt` || gotBody != "hello" || gotAuth != "Bearer vq_t" {
		t.Fatalf("%v %+v %q %q %q", err, out, gotName, gotBody, gotAuth)
	}
	var apiErr *client.Error
	err := c.Upload(t.Context(), "/up", "refuse.txt", strings.NewReader("x"), nil)
	if !errors.As(err, &apiErr) || apiErr.Status != 413 || apiErr.Message != "Files can be at most 10 MB." {
		t.Fatalf("%v", err)
	}
}

// VAQ-61: the saved config is 0600 even when a readable .tmp beside it was
// left by an earlier run.
func TestSaveIgnoresStaleTmpMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	path, err := client.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (client.Config{Profiles: []client.Profile{{Name: "team", URL: "https://vq.example", Token: "vq_x"}}}).Save(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st.Mode().Perm(), err)
	}
}

// VAQ-148: VQ_ADDR in {data dir}/vaquero.env moves where the CLI looks
// for the local server; the environment still wins.
func TestCLIFindsPortFromEnvFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vaquero.env"), []byte("VQ_ADDR=127.0.0.1:9123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"VQ_DATA_DIR": dir}
	get := func(k string) string { return env[k] }
	p, local, err := client.Resolve("local", get)
	if err != nil || !local || p.URL != "http://127.0.0.1:9123" {
		t.Fatalf("%+v %v", p, err)
	}
	env["VQ_ADDR"] = "127.0.0.1:9124"
	if p, _, err := client.Resolve("local", get); err != nil || p.URL != "http://127.0.0.1:9124" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := os.Chmod(filepath.Join(dir, "vaquero.env"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Resolve("local", get); err == nil || !strings.Contains(err.Error(), "vaquero.env can be read by other users") {
		t.Fatalf("%v", err)
	}
}
