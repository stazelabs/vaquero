package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/client"
)

// firstEntry is the name of the first file in a .tar.gz.
func firstEntry(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	h, err := tar.NewReader(zr).Next()
	if err != nil {
		t.Fatal(err)
	}
	return h.Name
}

func TestCLIExport(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Exported")
	file := filepath.Join(t.TempDir(), "out.tar.gz")
	// VAQ-61: a readable .tmp left by an earlier run doesn't lend its mode.
	if err := os.WriteFile(file+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustCLI(t, "export", file)
	if !strings.Contains(out, file) {
		t.Fatalf("%q", out)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if firstEntry(t, b) != "export.json" {
		t.Fatal("not an export")
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if _, err := os.Stat(file + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left: %v", err)
	}
	// - writes the archive, and only it, to standard output.
	if out := mustCLI(t, "export", "-"); firstEntry(t, []byte(out)) != "export.json" {
		t.Fatal("stdout is not an export")
	}
	if code, _, _ := cli(t, "export"); code != exitUsage {
		t.Fatalf("no file: %d", code)
	}
}

// A refused or broken download leaves nothing at FILE.
func TestCLIExportAtomic(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"refused", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"code":"missing_file","message":"The file for attachment X on VAQ-1 is missing from storage."}}`))
		}, "VAQ-1"},
		{"cut off", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/gzip")
			w.WriteHeader(http.StatusOK)
			w.Write(bytes.Repeat([]byte{0x1f}, 4096))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}, "broke off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			cfg := client.Config{Profiles: []client.Profile{{Name: "c", URL: srv.URL, Token: "vq_x"}}}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VAQUERO_PROFILE", "c")
			dir := t.TempDir()
			file := filepath.Join(dir, "out.tar.gz")
			code, _, errOut := cli(t, "export", file)
			if code != exitFail || !strings.Contains(errOut, tc.want) {
				t.Fatalf("%d %q", code, errOut)
			}
			if ents, _ := os.ReadDir(dir); len(ents) != 0 {
				t.Fatalf("left %v", ents)
			}
		})
	}
}
