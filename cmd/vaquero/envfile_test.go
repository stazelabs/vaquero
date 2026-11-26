package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/config/configtest"
	appdb "github.com/stazelabs/vaquero/internal/db"
)

// envFile reads dataDir/vaquero.env through the reader serve uses, and its
// mode.
func envFile(t *testing.T, dataDir string) (map[string]string, os.FileMode) {
	t.Helper()
	st, err := os.Stat(config.EnvFilePath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	vals, err := config.ReadEnvFile(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return vals, st.Mode().Perm()
}

// s3Env sets the S3 settings, with the sentinel keys.
func s3Env(t *testing.T) {
	t.Helper()
	t.Setenv("VQ_S3_ENDPOINT", "env.example.test:9000")
	t.Setenv("VQ_S3_BUCKET", "vq")
	t.Setenv("VQ_S3_ACCESS_KEY_ID", configtest.AccessKeyID)
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", configtest.Secret)
	t.Setenv("VQ_S3_REGION", "")
}

// VAQ-148: a move that switched writes the settings serve needs into
// vaquero.env, at 0600, keeping the lines it doesn't own; a failed move
// leaves the file as it was.
func TestMoveWritesEnvFile(t *testing.T) {
	withStores(t, nil)
	dataDir, _ := movedFrom(t, "sqlite")
	mine := "# my settings\nVQ_LOG_LEVEL=debug\n"
	if err := os.WriteFile(config.EnvFilePath(dataDir), []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}

	// A failed move: the file is untouched.
	saved := moveSnapshot
	moveSnapshot = func(context.Context, *sql.DB, string, appdb.Kind, time.Time) (appdb.BackupInfo, error) {
		return appdb.BackupInfo{}, errors.New("disk full")
	}
	if code, _, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 {
		t.Fatalf("%d %q", code, e)
	}
	moveSnapshot = saved
	if b, _ := os.ReadFile(config.EnvFilePath(dataDir)); string(b) != mine {
		t.Fatalf("a failed move changed the file: %q", b)
	}

	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 || !strings.Contains(out, config.EnvFilePath(dataDir)) {
		t.Fatalf("%d %q %q", code, out, e)
	}
	vals, mode := envFile(t, dataDir)
	// The default directory isn't written: it moves with the data
	// directory, as blob_location records it.
	if _, ok := vals["VQ_BLOB_DIR"]; mode != 0o600 || vals["VQ_BLOB_STORE"] != "dir" || ok || vals["VQ_LOG_LEVEL"] != "debug" {
		t.Fatalf("%o %v", mode, vals)
	}
	if b, _ := os.ReadFile(config.EnvFilePath(dataDir)); !strings.HasPrefix(string(b), mine) {
		t.Fatalf("lost the lines it doesn't own: %q", b)
	}

	s3Env(t)
	if code, out, e := runCLI(t, "attachments", "move", "--to", "s3"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	vals, mode = envFile(t, dataDir)
	want := map[string]string{
		"VQ_BLOB_STORE": "s3", "VQ_S3_ENDPOINT": "env.example.test:9000", "VQ_S3_BUCKET": "vq",
		"VQ_S3_ACCESS_KEY_ID": configtest.AccessKeyID, "VQ_S3_SECRET_ACCESS_KEY": configtest.Secret,
		"VQ_S3_REGION": "auto", "VQ_LOG_LEVEL": "debug",
	}
	if mode != 0o600 || !maps.Equal(vals, want) {
		t.Fatalf("%o %v", mode, vals)
	}

	// Back to the database: the S3 keys go, the old bucket read from the
	// file alone.
	for _, k := range []string{"VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY"} {
		t.Setenv(k, "")
	}
	if code, out, e := runCLI(t, "attachments", "move", "--to", "sqlite", "--delete-old"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	vals, _ = envFile(t, dataDir)
	if !maps.Equal(vals, map[string]string{"VQ_BLOB_STORE": "sqlite", "VQ_LOG_LEVEL": "debug"}) {
		t.Fatalf("%v", vals)
	}
	if left := objectKeys(t, fakeS3Store(t, "env.example.test:9000/vq")); len(left) != 0 {
		t.Fatalf("left in the bucket: %v", left)
	}
}

// VAQ-148: with nothing in the environment, serve boots on the storage the
// file names, and a file naming other storage is refused, saying so.
func TestServeBootsFromEnvFile(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatalf("serve from the file: %v", err)
	}
	if got := readSetting(t, dataDir, "blob_store"); got != "dir" {
		t.Fatalf("blob_store %q", got)
	}
	serving(t, dataDir, nil, func(base string) {
		tok, _ := os.ReadFile(filepath.Join(dataDir, "token"))
		for key, body := range want {
			id := key[strings.LastIndex(key, "/")+1:]
			req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/attachments/"+strings.ToUpper(id), nil)
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || string(b) != body {
				t.Fatalf("%d %q", resp.StatusCode, b)
			}
		}
	})

	if err := os.WriteFile(config.EnvFilePath(dataDir), []byte("VQ_BLOB_STORE=sqlite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := bootOnce(t, dataDir, nil)
	if err == nil || !strings.Contains(err.Error(), "Attachments are stored in dir; VQ_BLOB_STORE is sqlite (from vaquero.env). Set it back to dir.") {
		t.Fatalf("%v", err)
	}
	// The environment names no file.
	err = bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "sqlite"})
	if err == nil || !strings.Contains(err.Error(), "VQ_BLOB_STORE is sqlite. Set") {
		t.Fatalf("%v", err)
	}
	other := t.TempDir()
	if err := os.WriteFile(config.EnvFilePath(dataDir), []byte("VQ_BLOB_STORE=dir\nVQ_BLOB_DIR="+other+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = bootOnce(t, dataDir, nil)
	if err == nil || !strings.Contains(err.Error(), "VQ_BLOB_DIR is "+other+" (from vaquero.env).") {
		t.Fatalf("%v", err)
	}
}

// bootLogged serves on dir with extra env, logging to a buffer, runs fn
// while it is up and returns the log.
func bootLogged(t *testing.T, dir string, env map[string]string, fn func(base string)) string {
	t.Helper()
	addr := freeAddr(t)
	cfg, err := config.FromEnv(func(k string) string {
		switch k {
		case "VQ_ADDR":
			return addr
		case "VQ_DATA_DIR":
			return dir
		}
		return env[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	var log syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.NewTextHandler(&log, nil))) }()
	waitHealthy(t, addr)
	if fn != nil {
		fn("http://" + addr)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return log.String()
}

// s3EnvFile writes S3 storage with the sentinel keys into dir/vaquero.env.
func s3EnvFile(t *testing.T, dir string) {
	t.Helper()
	text := "VQ_BLOB_STORE=s3\nVQ_S3_ENDPOINT=env.example.test:9000\nVQ_S3_BUCKET=vq\n" +
		"VQ_S3_ACCESS_KEY_ID=" + configtest.AccessKeyID + "\nVQ_S3_SECRET_ACCESS_KEY=\"" + configtest.Secret + "\"\n"
	if err := os.WriteFile(config.EnvFilePath(dir), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// VAQ-148: the boot log names where each storage setting came from, never
// a value: neither the secret nor its redacted form.
func TestServeLogsSourcesNotValues(t *testing.T) {
	dir := t.TempDir()
	s3EnvFile(t, dir)
	log := bootLogged(t, dir, map[string]string{"VQ_S3_REGION": "auto"}, nil)
	for _, want := range []string{"VQ_BLOB_STORE=file", "VQ_BLOB_DIR=default", "VQ_S3_ENDPOINT=file", "VQ_S3_SECRET_ACCESS_KEY=file", "VQ_S3_REGION=environment", "attachments=s3"} {
		if !strings.Contains(log, want) {
			t.Fatalf("no %q in\n%s", want, log)
		}
	}
	configtest.NoLeak(t, "the boot log", log)
	for _, k := range []string{"VQ_S3_SECRET_ACCESS_KEY", "VQ_S3_ACCESS_KEY_ID"} {
		v := configtest.Secret
		if k == "VQ_S3_ACCESS_KEY_ID" {
			v = configtest.AccessKeyID
		}
		if strings.Contains(log, config.Redact(k, v)) {
			t.Fatalf("the boot log shows %s redacted:\n%s", k, log)
		}
	}
}

// VAQ-148, PR-1: the sentinel secret never shows in full on any surface
// that handles it: the move's output (text and JSON), the boot log and the
// pages and API answers of a server running with it.
func TestRedactNeverLeaks(t *testing.T) {
	withStores(t, nil)
	movedFrom(t, "sqlite")
	s3Env(t)
	code, out, e := runCLI(t, "attachments", "move", "--to", "s3")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	configtest.NoLeak(t, "the move's output", out+e)
	if !strings.Contains(out, config.Redact("VQ_S3_SECRET_ACCESS_KEY", configtest.Secret)) || !strings.Contains(out, config.Redact("VQ_S3_ACCESS_KEY_ID", configtest.AccessKeyID)) {
		t.Fatalf("the move doesn't show the redacted keys: %q", out)
	}
	// Moving again (nothing to do) and --json.
	_, out, e = runCLI(t, "attachments", "move", "--to", "s3", "--json")
	configtest.NoLeak(t, "the move's JSON", out+e)
	for _, k := range []string{"VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY"} {
		t.Setenv(k, "")
	}
	// A refusal that reads the file: the stopped server's settings.
	_, out, e = runCLI(t, "attachments", "move", "--to", "s3")
	configtest.NoLeak(t, "a refused move", out+e)

	dir := t.TempDir()
	s3EnvFile(t, dir)
	log := bootLogged(t, dir, nil, func(base string) {
		tok, _ := os.ReadFile(filepath.Join(dir, "token"))
		for _, path := range []string{"/admin", "/", "/api/v1/projects", "/api/v1/events"} {
			req, _ := http.NewRequest(http.MethodGet, base+path, nil)
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			configtest.NoLeak(t, path, string(b))
		}
	})
	configtest.NoLeak(t, "the boot log", log)
}

// Review I1: after a move to the default directory, the data directory
// moved whole still boots from the file, and a directory outside it is
// written as its path.
func TestMovedDataDirBootsFromEnvFile(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(dataDir, moved); err != nil {
		t.Fatal(err)
	}
	if err := bootOnce(t, moved, nil); err != nil {
		t.Fatalf("the moved data directory: %v", err)
	}
	if err := os.Rename(moved, dataDir); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "files")
	t.Setenv("VQ_BLOB_DIR", outside)
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if vals, _ := envFile(t, dataDir); vals["VQ_BLOB_DIR"] != outside {
		t.Fatalf("%v", vals)
	}
	t.Setenv("VQ_BLOB_DIR", "")
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatal(err)
	}
}

// Review I2: storage moved with environment settings (before vaquero.env)
// gets its file from a move to where it already is; a second run has
// nothing to save and is refused as before. Nothing shown holds a secret.
func TestMoveSavesSettingsForStorageInUse(t *testing.T) {
	withStores(t, nil)
	dataDir, _ := movedFrom(t, "sqlite")
	s3Env(t)
	if code, out, e := runCLI(t, "attachments", "move", "--to", "s3"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if err := os.Remove(config.EnvFilePath(dataDir)); err != nil {
		t.Fatal(err)
	}
	code, out, e := runCLI(t, "attachments", "move", "--to", "s3")
	if code != 0 || !strings.Contains(out, "Attachments are already stored there. Saved VQ_BLOB_STORE=s3") || !strings.Contains(out, config.EnvFilePath(dataDir)) {
		t.Fatalf("%d %q %q", code, out, e)
	}
	configtest.NoLeak(t, "the save's output", out+e)
	if vals, mode := envFile(t, dataDir); mode != 0o600 || vals["VQ_S3_SECRET_ACCESS_KEY"] != configtest.Secret || vals["VQ_BLOB_STORE"] != "s3" {
		t.Fatalf("%o %v", mode, vals)
	}
	code, out, e = runCLI(t, "attachments", "move", "--to", "s3", "--json")
	configtest.NoLeak(t, "the JSON", out+e)
	if code != 1 || !strings.Contains(e, "Attachments are already stored there.") {
		t.Fatalf("%d %q %q", code, out, e)
	}
}

// Review minor 6: restoring a snapshot whose storage isn't the one
// vaquero.env names says what to change.
func TestRestoreNotesEnvFileStorage(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	snaps := premoves(t, dataDir)
	code, out, e = runCLI(t, "backup", "restore", filepath.Base(snaps[0]))
	want := "vaquero.env sets VQ_BLOB_STORE=dir, but the restored database keeps its attachments in sqlite; set VQ_BLOB_STORE=sqlite there before starting vaquero."
	if code != 0 || !strings.Contains(out, want) {
		t.Fatalf("%d %q %q", code, out, e)
	}
}
