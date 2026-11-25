package vaquero_test

import (
	"os"
	"path/filepath"
	"slices"
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
