package vaquero_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// triggers is the event names a workflow's `on:` lists, whichever of the
// three YAML shapes it uses (a name, a list, a mapping).
func triggers(t *testing.T, doc map[string]any) []string {
	t.Helper()
	on, ok := doc["on"]
	if !ok {
		t.Fatal("no `on:` key")
	}
	switch v := on.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			s, _ := e.(string)
			out = append(out, s)
		}
		return out
	case map[string]any:
		var out []string
		for k := range v {
			out = append(out, k)
		}
		return out
	}
	t.Fatalf("unexpected `on:` shape %T", on)
	return nil
}

// Workflows run only by hand or when another workflow calls them: a push or
// pull request trigger would spend CI on every commit.
func TestWorkflowsHaveNoPushOrPullRequestTriggers(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("workflows: %v %v", files, err)
	}
	more, _ := filepath.Glob(".github/workflows/*.yaml")
	files = append(files, more...)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		got := triggers(t, doc)
		for _, bad := range []string{"push", "pull_request", "pull_request_target"} {
			if slices.Contains(got, bad) {
				t.Errorf("%s triggers on %s; workflows run only by hand or by call", f, bad)
			}
		}
	}
}

// The check itself: each shape of `on:` is read, and a push is caught.
func TestWorkflowTriggerShapes(t *testing.T) {
	for src, want := range map[string][]string{
		"on: push":                      {"push"},
		"on: [push, workflow_dispatch]": {"push", "workflow_dispatch"},
		"on:\n  pull_request: {}":       {"pull_request"},
		"on:\n  workflow_dispatch: {}":  {"workflow_dispatch"},
		"\"on\":\n  workflow_call: {}":  {"workflow_call"},
	} {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatal(err)
		}
		got := triggers(t, doc)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", src, got, want)
		}
	}
}

// pinnedRE is a third-party action at a full commit SHA; tagRE is the tag
// comment beside it (`# v5.1.0`).
var (
	pinnedRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)
	tagRE    = regexp.MustCompile(`^# v[0-9]+(\.[0-9]+)*$`)
)

// unpinned lists every `uses:` in a workflow that isn't a local workflow or
// action (./...) and isn't pinned to a full commit SHA with its tag in a
// trailing comment, as "line N: VALUE".
func unpinned(t *testing.T, src []byte) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		t.Fatal(err)
	}
	var bad []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if k.Value == "uses" && v.Kind == yaml.ScalarNode && !strings.HasPrefix(v.Value, "./") {
					if !pinnedRE.MatchString(v.Value) || !tagRE.MatchString(strings.TrimSpace(v.LineComment)) {
						bad = append(bad, fmt.Sprintf("line %d: %s %s", v.Line, v.Value, v.LineComment))
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	return bad
}

// A tag or branch can be moved to other code; a commit SHA can't. Every
// third-party action is pinned to one, with the tag it was resolved from
// in a comment (docs/RELEASING.md says how to move one).
func TestWorkflowActionsArePinnedBySHA(t *testing.T) {
	files, _ := filepath.Glob(".github/workflows/*.y*ml")
	if len(files) == 0 {
		t.Fatal("no workflows")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range unpinned(t, b) {
			t.Errorf("%s %s: pin the action to a commit SHA with its tag in a comment", f, u)
		}
	}
}

// The check itself: tags, branches, short SHAs and a missing comment are
// caught; local workflows and pinned actions pass, in steps and jobs.
func TestUnpinnedShapes(t *testing.T) {
	sha := "fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09"
	for src, want := range map[string]int{
		"steps:\n  - uses: actions/checkout@v5":                                             1,
		"steps:\n  - uses: actions/checkout@main":                                           1,
		"steps:\n  - uses: actions/checkout@fbc6f39 # v5.1.0":                               1,
		"steps:\n  - uses: actions/checkout@" + sha:                                         1,
		"steps:\n  - uses: actions/checkout@" + sha + " # main":                             1,
		"steps:\n  - uses: actions/checkout@" + sha + " # v5.1.0":                           0,
		"jobs:\n  gate:\n    uses: ./.github/workflows/quality-gate.yml":                    0,
		"jobs:\n  gate:\n    uses: other/repo/.github/workflows/x.yml@v1":                   1,
		"jobs:\n  a:\n    steps:\n      - uses: a/b@" + sha + " # v1\n      - uses: c/d@v2": 1,
	} {
		if got := unpinned(t, []byte(src)); len(got) != want {
			t.Errorf("%q: got %v, want %d", src, got, want)
		}
	}
}
