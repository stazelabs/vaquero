package core_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestPatchTicketTier(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	tk, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Tier: new("standard")}, f.T1.Version)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Tier != "standard" || tk.Version != f.T1.Version+1 {
		t.Fatalf("tier %q version %d", tk.Tier, tk.Version)
	}
	if n := eventCount(t, d); n != before+1 {
		t.Fatalf("%d events, want 1", n-before)
	}
	kind, payload := lastEvent(t, d)
	changes, _ := payload["changes"].(map[string]any)
	tier, _ := changes["tier"].(map[string]any)
	if kind != core.EvTicketUpdated || tier["from"] != "" || tier["to"] != "standard" {
		t.Fatalf("%s %v", kind, payload)
	}

	// The same tier again changes nothing.
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Tier: new("standard")}, 0); err != nil || eventCount(t, d) != before+1 {
		t.Fatalf("no-op: %v", err)
	}

	_, err = c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Tier: new("huge")}, 0)
	fieldErr(t, err, "tier", "Choose design, light, standard or heavy.")

	tk, err = c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Model: new("  some-model-1  ")}, 0)
	if err != nil || tk.Model != "some-model-1" || tk.Tier != "standard" {
		t.Fatalf("%v %q %q", err, tk.Model, tk.Tier)
	}
	_, err = c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Model: new(strings.Repeat("m", 81))}, 0)
	fieldErr(t, err, "model", "Keep the model under 80 characters.")

	// A full update from the form keeps what it carries.
	tk, err = c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, core.TicketInput{Title: "First", Tags: []string{"bug"}, Tier: "", Model: ""})
	if err != nil || tk.Tier != "" || tk.Model != "" {
		t.Fatalf("%v %q %q", err, tk.Tier, tk.Model)
	}

	nt, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Tiered", Tier: "design"})
	if err != nil || nt.Tier != "design" {
		t.Fatalf("%v %q", err, nt.Tier)
	}
}

func TestTierMapDefaultAndParse(t *testing.T) {
	def := core.DefaultTierMap()
	if got := def.Model("claude", "heavy"); got != "claude-opus-5-5" {
		t.Fatalf("claude heavy = %q", got)
	}
	if def.Model("claude", "design") != def.Model("claude", "heavy") || def.Model("codex", "design") != def.Model("codex", "heavy") {
		t.Fatalf("design is not the heavy model: %v", def)
	}
	if def.Model("nobody", "heavy") != "" || def.Model("claude", "huge") != "" {
		t.Fatal("unknown harness or tier names a model")
	}
	// Changing the copy leaves the default alone.
	def["claude"]["heavy"] = "x"
	if core.DefaultTierMap().Model("claude", "heavy") != "claude-opus-5-5" {
		t.Fatal("DefaultTierMap shares its maps")
	}

	m, err := core.ParseTierMap(`{"claude": {"light": "a", "heavy": "b"}, "other": {}}`)
	if err != nil || m.Model("claude", "light") != "a" || m.Model("claude", "standard") != "" {
		t.Fatalf("%v %v", err, m)
	}
	if m, err := core.ParseTierMap(""); err != nil || m.Model("claude", "heavy") != "claude-opus-5-5" {
		t.Fatalf("empty: %v %v", err, m)
	}
	for in, want := range map[string]string{
		`{"claude": `:               "Enter the tier map as JSON: each harness names a model for any of design, light, standard and heavy.",
		`["claude"]`:                "Enter the tier map as JSON: each harness names a model for any of design, light, standard and heavy.",
		`{"claude": {"heavy": 1}}`:  "Enter the tier map as JSON: each harness names a model for any of design, light, standard and heavy.",
		`{"claude": {"huge": "x"}}`: "Use only the tiers design, light, standard and heavy.",
		`{"": {"heavy": "x"}}`:      "Name each harness.",
		`{"claude": {"heavy": "` + strings.Repeat("m", 81) + `"}}`: "Keep each model under 80 characters.",
	} {
		_, err := core.ParseTierMap(in)
		fieldErr(t, err, "tier_map", want)
	}
}

func TestProjectWorkflowSwitch(t *testing.T) {
	c, d, f := newFixture(t)
	if f.Project.Workflow != 0 || f.Project.TierMap != "" {
		t.Fatalf("new project: %d %q", f.Project.Workflow, f.Project.TierMap)
	}
	before := eventCount(t, d)
	p, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{Workflow: new(true)})
	if err != nil || p.Workflow != 1 || p.Name != "Vaquero" {
		t.Fatalf("%v %+v", err, p)
	}
	if n := eventCount(t, d); n != before+1 {
		t.Fatalf("%d events, want 1", n-before)
	}
	kind, payload := lastEvent(t, d)
	changes, _ := payload["changes"].(map[string]any)
	wf, _ := changes["workflow"].(map[string]any)
	if kind != core.EvProjectUpdated || wf["from"] != false || wf["to"] != true {
		t.Fatalf("%s %v", kind, payload)
	}
	// On again is no change.
	if _, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{Workflow: new(true)}); err != nil || eventCount(t, d) != before+1 {
		t.Fatalf("no-op: %v", err)
	}

	// The tier map is stored canonical; the default, typed out, stores "".
	p, err = c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{TierMap: new(`{"claude": {"heavy": "big"}}`)})
	if err != nil || p.TierMap != `{"claude":{"heavy":"big"}}` || eventCount(t, d) != before+2 {
		t.Fatalf("%v %q", err, p.TierMap)
	}
	p, err = c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{TierMap: new(core.DefaultTierMap().String())})
	if err != nil || p.TierMap != "" || eventCount(t, d) != before+3 {
		t.Fatalf("%v %q", err, p.TierMap)
	}
	if _, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{TierMap: new("")}); err != nil || eventCount(t, d) != before+3 {
		t.Fatalf("default again: %v", err)
	}
	_, err = c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{TierMap: new("{")})
	fieldErr(t, err, "tier_map", "")
	if eventCount(t, d) != before+3 {
		t.Fatal("a refused map wrote")
	}
}
