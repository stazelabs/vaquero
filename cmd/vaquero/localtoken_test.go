package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// VAQ-61: the token file must name a live token of the owner's own; one of
// another actor's is replaced. A new local token revokes the old "local"
// ones, which the replaced file may have leaked. The new file is 0600 even
// when a stale .tmp beside it was readable.
func TestEnsureLocalToken(t *testing.T) {
	d := dbtest.New(t)
	c := core.New(d.W, time.Now)
	owner, err := c.EnsureOwner(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := c.EnsureAgent(t.Context(), owner.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, TokenFile)
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	if err := ensureLocalToken(t.Context(), c, d.Q, owner, dir); err != nil {
		t.Fatal(err)
	}
	first := read()
	// Another actor's token in the file is not the owner's.
	raw, _, err := c.CreateToken(t.Context(), owner.ID, agent.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalToken(t.Context(), c, d.Q, owner, dir); err != nil {
		t.Fatal(err)
	}
	second := read()
	if second == raw || second == first {
		t.Fatal("the token file wasn't replaced with a new owner token")
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v %v", st.Mode().Perm(), err)
	}
	toks, err := d.Q.ListTokens(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]int{}
	for _, tk := range toks {
		if !tk.RevokedAt.Valid {
			live[tk.Name]++
		}
	}
	if live["local"] != 1 || live["builder"] != 1 {
		t.Fatalf("live tokens %v, want one local and the agent's", live)
	}
}
