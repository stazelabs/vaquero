package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/core"
)

var tinyPNG = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{7}, 64)...)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLIAttach(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Has files")
	png := writeFile(t, "shot one.png", tinyPNG)
	txt := writeFile(t, "notes.txt", []byte("plain notes"))
	out := mustCLI(t, "attachment", "new", "vaq-1", png, txt)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 ||
		!regexp.MustCompile(`^!\[shot one\.png\]\(/attachments/[0-9A-Z]{26}/shot%20one\.png\)$`).MatchString(lines[0]) ||
		!regexp.MustCompile(`^\[notes\.txt\]\(/attachments/[0-9A-Z]{26}/notes\.txt\)$`).MatchString(lines[1]) {
		t.Fatalf("%q", out)
	}

	// --json prints the list of attachments, and flags may follow files.
	var got []api.Attachment
	if err := json.Unmarshal([]byte(mustCLI(t, "attachment", "new", "VAQ-1", txt, "--json")), &got); err != nil ||
		len(got) != 1 || got[0].Name != "notes.txt" || got[0].Size != 11 || got[0].ID == "" {
		t.Fatalf("%v %+v", err, got)
	}

	// attachments: a table.
	table := mustCLI(t, "attachment", "list", "VAQ-1")
	rows := strings.Split(strings.TrimSpace(table), "\n")
	if len(rows) != 4 || !regexp.MustCompile(`^ID\s+NAME\s+SIZE\s+BY\s+WHEN$`).MatchString(rows[0]) ||
		!strings.Contains(table, "shot one.png") || !strings.Contains(table, "1 KB") || !strings.Contains(table, "Owner") || !strings.Contains(table, "UTC") {
		t.Fatalf("%q", table)
	}
	var list []api.Attachment
	if err := json.Unmarshal([]byte(mustCLI(t, "attachment", "list", "VAQ-1", "--json")), &list); err != nil || len(list) != 3 {
		t.Fatalf("%v %+v", err, list)
	}

	// stdin needs --name.
	stdin = strings.NewReader("from a pipe")
	defer func() { stdin = nil }()
	if code, _, errOut := cli(t, "attachment", "new", "VAQ-1", "-"); code != 2 || !strings.Contains(errOut, "--name") {
		t.Fatalf("no name: %d %q", code, errOut)
	}
	stdin = strings.NewReader("from a pipe")
	if out := mustCLI(t, "attachment", "new", "VAQ-1", "-", "--name", "pipe.txt"); !strings.HasPrefix(out, "[pipe.txt](/attachments/") {
		t.Fatalf("%q", out)
	}

	// detach removes one; a second detach says so.
	mustCLI(t, "attachment", "delete", list[0].ID)
	if code, _, errOut := cli(t, "attachment", "delete", list[0].ID); code != 1 || errOut == "" {
		t.Fatalf("detach twice: %d %q", code, errOut)
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "attachment", "list", "VAQ-1", "--json")), &list); err != nil || len(list) != 3 {
		t.Fatalf("%v %d", err, len(list))
	}

	// Refusals: a missing file, a missing ticket.
	if code, _, errOut := cli(t, "attachment", "new", "VAQ-1", filepath.Join(t.TempDir(), "nope")); code != 1 || errOut == "" {
		t.Errorf("missing file: %d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "attachment", "new", "VAQ-9", txt); code != 1 || !strings.Contains(errOut, "No ticket VAQ-9") {
		t.Errorf("missing ticket: %d %q", code, errOut)
	}
}

func TestCLIAttachTooLarge(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "attachments") {
			calls.Add(1)
		}
		http.Error(w, "{}", http.StatusInternalServerError)
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	cfg := client.Config{Profiles: []client.Profile{{Name: "c", URL: srv.URL, Token: "vq_x"}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAQUERO_PROFILE", "c")
	big := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(core.MaxAttachment + 1); err != nil { // sparse
		t.Fatal(err)
	}
	f.Close()
	code, _, errOut := cli(t, "attachment", "new", "VAQ-1", big)
	if code != 1 || !strings.Contains(errOut, "Files can be at most 10 MB.") || !strings.Contains(errOut, "big.bin") {
		t.Fatalf("%d %q", code, errOut)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d upload requests made", n)
	}
	// Standard input over the limit is refused as well.
	stdin = strings.NewReader(strings.Repeat("x", core.MaxAttachment+1))
	defer func() { stdin = nil }()
	if code, _, errOut := cli(t, "attachment", "new", "VAQ-1", "-", "--name", "big.txt"); code != 1 || !strings.Contains(errOut, "Files can be at most 10 MB.") || calls.Load() != 0 {
		t.Fatalf("stdin: %d %q %d", code, errOut, calls.Load())
	}
}

// The server's refusal reaches the person as its message (exit 1).
func TestCLIAttachServerRefusal(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Has files")
	empty := writeFile(t, "empty.txt", nil)
	if code, _, errOut := cli(t, "attachment", "new", "VAQ-1", empty); code != 1 || strings.TrimSpace(errOut) == "" {
		t.Fatalf("%d %q", code, errOut)
	}
}
