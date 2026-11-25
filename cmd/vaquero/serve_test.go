package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/sweep"
	_ "modernc.org/sqlite"
)

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func testConfig(t *testing.T, addr, dir string) config.Config {
	// Config refuses port 0, but a test may want the system to pick one.
	valid := strings.TrimSuffix(addr, ":0")
	if valid != addr {
		valid += ":7878"
	}
	cfg, err := config.FromEnv(func(k string) string { return map[string]string{"VQ_ADDR": valid, "VQ_DATA_DIR": dir}[k] })
	if err != nil {
		t.Fatal(err)
	}
	cfg.Addr = addr
	return cfg
}

func TestServeStartsAndStopsCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "vaquero") // does not exist yet
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, dir), slog.New(slog.DiscardHandler)) }()
	var resp *http.Response
	var err error
	for range 50 {
		if resp, err = http.Get("http://" + addr + "/healthz"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("%q", b)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("data dir: %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "vaquero.db-wal")); err == nil && fi.Size() != 0 {
		t.Fatal("WAL not checkpointed")
	}
}

// Review Focus 4
func TestServeReportsAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	dir := filepath.Join(t.TempDir(), "vaquero")
	err = serve(context.Background(), testConfig(t, addr, dir), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), addr) || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a failed second instance touched the data directory: %v", err)
	}
}

// Review Focus 4: an open live-update stream doesn't hold up shutdown.
func TestServeStopsPromptlyWithOpenStream(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, t.TempDir()), slog.New(slog.DiscardHandler)) }()
	var res *http.Response
	var err error
	for range 50 {
		if res, err = http.Get("http://" + addr + "/events/stream"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve held open by the stream")
	}
}

func waitHealthy(t *testing.T, addr string) {
	t.Helper()
	for range 60 {
		if resp, err := http.Get("http://" + addr + "/healthz"); err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server never answered")
}

func TestServeWritesLocalToken(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		addr := freeAddr(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- serve(ctx, testConfig(t, addr, dir), slog.New(slog.DiscardHandler)) }()
		waitHealthy(t, addr)
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Stat(filepath.Join(dir, "token"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "token"))
	if !strings.HasPrefix(string(b), "vq_") {
		t.Fatalf("%q", b)
	}
	w, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "vaquero.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var n int
	w.QueryRow(`SELECT count(*) FROM api_tokens WHERE name = 'local' AND revoked_at IS NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d local tokens after two boots", n)
	}
}

// Review Focus 3.
func TestServeRefusesSharedDataDir(t *testing.T) {
	dir := t.TempDir()
	first := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, first, dir), slog.New(slog.DiscardHandler)) }()
	waitHealthy(t, first)
	err := serve(context.Background(), testConfig(t, freeAddr(t), dir), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "another vaquero is using") || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("%v", err)
	}
	if resp, err := http.Get("http://" + first + "/healthz"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("first server hurt: %v", err)
	}
	cancel()
	<-done
}

// The sweep runs while serving and is stopped before serve returns.
func TestServeStopsSweeper(t *testing.T) {
	var ticks atomic.Int64
	sweepInterval = 10 * time.Millisecond
	sweepOnTick = func() { ticks.Add(1) }
	defer func() { sweepInterval, sweepOnTick = sweep.Every, nil }()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, t.TempDir()), slog.New(slog.DiscardHandler)) }()
	deadline := time.Now().Add(5 * time.Second)
	for ticks.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ticks.Load() < 2 {
		t.Fatal("the sweep never ran")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after := ticks.Load()
	time.Sleep(50 * time.Millisecond)
	if ticks.Load() != after {
		t.Fatal("the sweep ran after serve returned")
	}
}

// bootOnce serves on dir with extra env, waits until healthy and stops it.
func bootOnce(t *testing.T, dir string, env map[string]string) error {
	t.Helper()
	addr := freeAddr(t)
	get := func(k string) string {
		if k == "VQ_ADDR" {
			return addr
		}
		if k == "VQ_DATA_DIR" {
			return dir
		}
		return env[k]
	}
	cfg, err := config.FromEnv(get)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.DiscardHandler)) }()
	for range 100 {
		select {
		case err := <-done:
			cancel()
			return err
		default:
		}
		if resp, err := http.Get("http://" + addr + "/healthz"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	return <-done
}

func readSetting(t *testing.T, dir, key string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestInstanceIDStable(t *testing.T) {
	dir := t.TempDir()
	if err := bootOnce(t, dir, nil); err != nil {
		t.Fatal(err)
	}
	first := readSetting(t, dir, "instance_id")
	if first == "" || first != strings.ToLower(first) {
		t.Fatalf("instance id %q", first)
	}
	if err := bootOnce(t, dir, nil); err != nil {
		t.Fatal(err)
	}
	if second := readSetting(t, dir, "instance_id"); second != first {
		t.Fatalf("%q then %q", first, second)
	}
}

// addAttachmentRow writes one attachments row straight into the database,
// with foreign keys off (the test needs a row, not a ticket).
func addAttachmentRow(t *testing.T, dir string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO attachments (id, ticket_id, uploaded_by, name, content_type, size, blob_key, created_at)
		VALUES ('a1', 't1', 'u1', 'x.png', 'image/png', 1, 'attachments/i/a1', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
}

func settingsDump(t *testing.T, dir string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var k, v, u string
		if err := rows.Scan(&k, &v, &u); err != nil {
			t.Fatal(err)
		}
		b.WriteString(k + "=" + v + "@" + u + "\n")
	}
	return b.String()
}

// tableCounts is "table=rows ..." for the named tables.
func tableCounts(t *testing.T, dir string, tables ...string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var b strings.Builder
	for _, tbl := range tables {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s=%d ", tbl, n)
	}
	return b.String()
}

func TestServeRefusesBackendChange(t *testing.T) {
	dir := t.TempDir()
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "sqlite"}); err != nil {
		t.Fatal(err)
	}
	addAttachmentRow(t, dir)
	// A refused boot writes nothing: not the owner, not a token.
	tokenFile := filepath.Join(dir, "token")
	if err := os.Remove(tokenFile); err != nil {
		t.Fatal(err)
	}
	before := settingsDump(t, dir)
	rowsBefore := tableCounts(t, dir, "actors", "api_tokens", "events")
	err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir"})
	want := "Attachments are stored in sqlite; VQ_BLOB_STORE is dir. Set it back to sqlite."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v", err)
	}
	if after := settingsDump(t, dir); after != before {
		t.Fatalf("database changed:\n%s\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, "attachments")); err == nil {
		t.Fatal("the refused backend's directory was created")
	}
	if _, err := os.Stat(tokenFile); err == nil {
		t.Fatal("the refused boot wrote the token file")
	}
	if rowsAfter := tableCounts(t, dir, "actors", "api_tokens", "events"); rowsAfter != rowsBefore {
		t.Fatalf("the refused boot wrote rows: %s then %s", rowsBefore, rowsAfter)
	}
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "sqlite"}); err != nil {
		t.Fatalf("the recorded backend must still boot: %v", err)
	}
}

func TestServeAllowsBackendChangeWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "sqlite"}); err != nil {
		t.Fatal(err)
	}
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir"}); err != nil {
		t.Fatal(err)
	}
	if got := readSetting(t, dir, "blob_store"); got != "dir" {
		t.Fatalf("blob_store = %q", got)
	}
}

// VAQ-78: the lock covers where the bytes are, not only the backend: a dir
// store moved to another VQ_BLOB_DIR while attachments exist is refused,
// naming both, and the recorded location still boots.
func TestServeRefusesBlobLocationChange(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": a}); err != nil {
		t.Fatal(err)
	}
	if got := readSetting(t, dir, "blob_location"); got != a {
		t.Fatalf("blob_location = %q, want %q", got, a)
	}
	addAttachmentRow(t, dir)
	before := settingsDump(t, dir)
	err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": b})
	want := "Attachments are stored in " + a + "; VQ_BLOB_DIR is " + b + ". Set it back to " + a + "."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v", err)
	}
	if after := settingsDump(t, dir); after != before {
		t.Fatalf("database changed:\n%s\n%s", before, after)
	}
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": a}); err != nil {
		t.Fatalf("the recorded location must still boot: %v", err)
	}
}

// VAQ-78: a database from before the location was recorded is not refused:
// the next boot records it.
func TestServeRecordsMissingBlobLocation(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(t.TempDir(), "a")
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": a}); err != nil {
		t.Fatal(err)
	}
	addAttachmentRow(t, dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM settings WHERE key = 'blob_location'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	b := filepath.Join(t.TempDir(), "b")
	if err := bootOnce(t, dir, map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": b}); err != nil {
		t.Fatalf("an unrecorded location was refused: %v", err)
	}
	if got := readSetting(t, dir, "blob_location"); got != b {
		t.Fatalf("blob_location = %q, want %q", got, b)
	}
}

// VAQ-78: an S3 store is named by endpoint and bucket; SQLite storage has
// no location.
func TestBlobLocationRefusals(t *testing.T) {
	s3 := func(ep, bucket string) config.Config {
		return config.Config{BlobBackend: "s3", S3Endpoint: ep, S3Bucket: bucket}
	}
	if got := blobs.Location(s3("S3.example.com", "files")); got != "s3.example.com/files" {
		t.Fatalf("s3 location %q", got)
	}
	if got := blobs.Location(config.Config{BlobBackend: "sqlite", BlobDir: "/x"}); got != "" {
		t.Fatalf("sqlite location %q", got)
	}
	err := blobLocationRefusal(s3("s3.example.com", "other"), "s3.example.com/files")
	want := "Attachments are stored in s3.example.com/files; VQ_S3_ENDPOINT and VQ_S3_BUCKET name s3.example.com/other. Set them back to s3.example.com and files."
	if err == nil || err.Error() != want {
		t.Fatalf("%v", err)
	}
}

// VAQ-61: a holder whose pid couldn't be read isn't "(pid 0)".
func TestInUseMessage(t *testing.T) {
	if got := inUse("/d", 42).Error(); got != "another vaquero is using /d (pid 42)" {
		t.Errorf("%q", got)
	}
	if got := inUse("/d", 0).Error(); got != "another vaquero is using /d" {
		t.Errorf("%q", got)
	}
}

// VAQ-78 review: a data directory moved whole, with dir storage at its
// default place inside it, still boots and serves its attachments: the
// location is recorded relative to the data directory.
func TestMovedDataDirKeepsDefaultBlobDir(t *testing.T) {
	dataDir, addr := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "dir")
	runCLI(t, "projects")
	if code, _, e := runCLI(t, "project", "create", "VAQ", "Vaquero"); code != 0 {
		t.Fatal(e)
	}
	if code, _, e := runCLI(t, "ticket", "new", "VAQ", "x"); code != 0 {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("moved bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, e := runCLI(t, "attach", "VAQ-1", file, "--json")
	if code != 0 {
		t.Fatal(e)
	}
	var atts []struct{ ID string }
	if err := json.Unmarshal([]byte(out), &atts); err != nil || len(atts) != 1 {
		t.Fatalf("%v %q", err, out)
	}
	att := atts[0]
	if got := readSetting(t, dataDir, "blob_location"); got != "{data dir}/attachments" {
		t.Fatalf("blob_location = %q", got)
	}
	runCLI(t, "stop")
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(dataDir, moved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VQ_DATA_DIR", moved)
	t.Cleanup(func() { killTestServer(moved) })
	if code, _, e := runCLI(t, "attachments", "VAQ-1"); code != 0 {
		t.Fatalf("the moved data directory didn't serve: %s", e)
	}
	tok, _ := os.ReadFile(filepath.Join(moved, "token"))
	req, _ := http.NewRequest("GET", "http://"+addr+"/api/v1/attachments/"+att.ID, nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != "moved bytes" {
		t.Fatalf("%d %q", res.StatusCode, body)
	}
}

// An external VQ_BLOB_DIR is recorded absolute; changing it is refused.
func TestBlobLocationOutsideDataDirIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, BlobBackend: "dir", BlobDir: filepath.Join(dir, "attachments")}
	if got := blobs.Location(cfg); got != "{data dir}/attachments" {
		t.Fatalf("%q", got)
	}
	cfg.BlobDir = filepath.Join(dir, "..", filepath.Base(dir)+"-sibling")
	if got := blobs.Location(cfg); !filepath.IsAbs(got) || strings.Contains(got, "..") {
		t.Fatalf("%q", got)
	}
	if err := blobLocationRefusal(config.Config{DataDir: "/new", BlobBackend: "dir", BlobDir: "/elsewhere"}, "{data dir}/attachments"); err == nil ||
		err.Error() != "Attachments are stored in /new/attachments; VQ_BLOB_DIR is /elsewhere. Set it back to /new/attachments." {
		t.Fatalf("%v", err)
	}
}
