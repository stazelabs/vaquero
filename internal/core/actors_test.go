package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops, coreOp{"EnsureAgent",
		func(c *core.Core, f fixture) error { _, err := c.EnsureAgent(bg, f.Owner.ID, "builder"); return err },
		func(c *core.Core, f fixture) error { _, err := c.EnsureAgent(bg, f.Owner.ID, "bad/name"); return err }})
}

func TestEnsureAgentIdempotent(t *testing.T) {
	c, d, owner := newCore(t)
	before := eventCount(t, d)
	a, err := c.EnsureAgent(bg, owner.ID, "builder")
	if err != nil || a.Kind != "agent" || a.Name != "builder" {
		t.Fatalf("%+v %v", a, err)
	}
	b, err := c.EnsureAgent(bg, owner.ID, "  builder ")
	if err != nil || b.ID != a.ID {
		t.Fatalf("second call: %+v %v", b, err)
	}
	if n := eventCount(t, d) - before; n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

func TestAgentNameRules(t *testing.T) {
	c, _, owner := newCore(t)
	for _, good := range []string{"builder", "Claude Code 2", "agent_1.test-x", "café", strings.Repeat("a", 80)} {
		if _, err := c.EnsureAgent(bg, owner.ID, good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", 81), "a/b", "tab\there", "line\nbreak", "<script>", "nul\x00"} {
		_, err := c.EnsureAgent(bg, owner.ID, bad)
		var inv *core.Invalid
		if !errors.As(err, &inv) || inv.Fields["agent"] == "" {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
