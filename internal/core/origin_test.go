package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

// newestMeta is the newest event's meta, decoded.
func newestMeta(t *testing.T, c *core.Core) map[string]string {
	t.Helper()
	var raw string
	if err := c.DB.QueryRow(`SELECT meta FROM events ORDER BY seq DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("meta %q: %v", raw, err)
	}
	return m
}

func TestEmitRecordsOrigin(t *testing.T) {
	c, _, owner := newCore(t)
	ctx := core.WithOrigin(context.Background(), core.Origin{Surface: "mcp", Harness: "claude-code/2.1.3", Model: "claude-opus-5-5"})
	if _, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"}); err != nil {
		t.Fatal(err)
	}
	m := newestMeta(t, c)
	want := map[string]string{"surface": "mcp", "harness": "claude-code/2.1.3", "model": "claude-opus-5-5"}
	if len(m) != len(want) {
		t.Fatalf("meta %v, want %v (empty fields left out)", m, want)
	}
	for k, v := range want {
		if m[k] != v {
			t.Fatalf("meta %v, want %v", m, want)
		}
	}

	long := strings.Repeat("é", 100)
	ctx = core.WithOrigin(context.Background(), core.Origin{Surface: "api", Client: long, Harness: long, Model: long, Run: long})
	if _, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "LONG", Name: "Long"}); err != nil {
		t.Fatal(err)
	}
	m = newestMeta(t, c)
	for _, k := range []string{"client", "harness", "model", "run"} {
		if got := m[k]; got != strings.Repeat("é", 80) {
			t.Fatalf("%s = %q (%d runes), want 80 characters", k, got, len([]rune(got)))
		}
	}
}

func TestEmitWithoutOriginStoresEmptyMeta(t *testing.T) {
	c, _, owner := newCore(t)
	if _, err := c.CreateProject(context.Background(), owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"}); err != nil {
		t.Fatal(err)
	}
	if m := newestMeta(t, c); len(m) != 0 {
		t.Fatalf("meta %v, want {}", m)
	}
	if o := core.OriginFrom(context.Background()); o != (core.Origin{}) {
		t.Fatalf("%+v", o)
	}
}
