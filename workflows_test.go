package vaquero_test

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
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

// fakeGH is a stand-in for the gh CLI on PATH: it logs each call to
// $FAKE_GH_LOG and answers the tap's contents API as $FAKE_GH_MODE says:
// "same" (the file's blob sha is $FAKE_GH_SHA), "other" (another sha),
// "missing" (404) or "broken" (401).
const fakeGH = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GH_LOG"
if [ "$1" = api ] && [ "$3" = --jq ]; then
  case "$FAKE_GH_MODE" in
    same) echo "$FAKE_GH_SHA" ;;
    other) echo 1111111111111111111111111111111111111111 ;;
    missing) echo '{"message":"Not Found"}'; echo 'gh: Not Found (HTTP 404)' >&2; exit 1 ;;
    *) echo 'gh: Bad credentials (HTTP 401)' >&2; exit 1 ;;
  esac
  exit 0
fi
if [ "$1" = api ] && [ "$2" = --method ]; then
  echo "Formula/vaquero.rb updated: https://example.invalid/commit"
  exit 0
fi
echo "unexpected gh call: $*" >&2
exit 2
`

// publishFormula runs scripts/publish-formula.sh for v0.1.2 (the formula
// golden file's release) against fakeGH in mode, returning its exit code,
// output and the gh calls it made.
func publishFormula(t *testing.T, mode string) (int, string, []string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o700); err != nil {
		t.Fatal(err)
	}
	// The golden file's git blob id, as git hash-object gives it (only
	// internal/gitread runs git).
	golden, err := os.ReadFile("scripts/formula/testdata/vaquero.rb.golden")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(golden))), golden...))
	sha := hex.EncodeToString(sum[:])
	log := filepath.Join(t.TempDir(), "gh.log")
	cmd := exec.Command("scripts/publish-formula.sh", "v0.1.2", "scripts/formula/testdata/checksums.txt")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GH_LOG="+log, "FAKE_GH_MODE="+mode, "FAKE_GH_SHA="+sha)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	return code, string(out), strings.Split(strings.TrimSpace(string(b)), "\n")
}

// The formula step, shared by the release and formula workflows: a tap
// already holding this release's formula is left alone, a missing or other
// one is written, and any other error stops it.
func TestPublishFormula(t *testing.T) {
	if testing.Short() {
		t.Skip("builds scripts/formula")
	}
	path := "repos/stazelabs/homebrew-vaquero/contents/Formula/vaquero.rb"
	code, out, calls := publishFormula(t, "same")
	if code != 0 || !strings.Contains(out, "The tap already holds the v0.1.2 formula; nothing changed.") || len(calls) != 1 {
		t.Errorf("same: %d %q %v", code, out, calls)
	}
	code, out, calls = publishFormula(t, "missing")
	if code != 0 || !strings.Contains(out, "Formula/vaquero.rb updated") || len(calls) != 2 ||
		!strings.HasPrefix(calls[1], "api --method PUT "+path+" -f message=vaquero v0.1.2 -f content=") || strings.Contains(calls[1], "sha=") {
		t.Errorf("missing: %d %q %v", code, out, calls)
	}
	code, out, calls = publishFormula(t, "other")
	if code != 0 || len(calls) != 2 || !strings.Contains(calls[1], "-f sha=1111111111111111111111111111111111111111") {
		t.Errorf("other: %d %q %v", code, out, calls)
	}
	code, out, calls = publishFormula(t, "broken")
	if code == 0 || !strings.Contains(out, "HTTP 401") || len(calls) != 1 {
		t.Errorf("broken: %d %q %v", code, out, calls)
	}
}

// Both workflows publish the formula through the one script, and the
// formula workflow runs only by hand.
func TestFormulaWorkflowsShareTheScript(t *testing.T) {
	for _, f := range []string{".github/workflows/release.yml", ".github/workflows/formula.yml"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "scripts/publish-formula.sh") || strings.Contains(string(b), "--method PUT") {
			t.Errorf("%s doesn't publish through scripts/publish-formula.sh", f)
		}
	}
	st, err := os.Stat("scripts/publish-formula.sh")
	if err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("scripts/publish-formula.sh isn't executable: %v", err)
	}
}

// VAQ-74: with no pull request triggers, CONTRIBUTING.md is where a
// contributor learns to run the gate locally and that a maintainer runs it
// on the branch; the README links it.
func TestContributingExplainsTheManualGate(t *testing.T) {
	b, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"make ci", "ci workflow", "pull_request"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("CONTRIBUTING.md doesn't mention %q", want)
		}
	}
	r, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r), "(CONTRIBUTING.md)") {
		t.Error("README.md doesn't link CONTRIBUTING.md")
	}
}

// tagCheck is the run script of the "Tag is a release number" step in a
// workflow.
func tagCheck(t *testing.T, file string) (script, shell string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Jobs map[string]struct {
			Steps []struct {
				Name  string `yaml:"name"`
				Run   string `yaml:"run"`
				Shell string `yaml:"shell"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, j := range doc.Jobs {
		for _, s := range j.Steps {
			if s.Name == "Tag is a release number" {
				return s.Run, s.Shell
			}
		}
	}
	t.Fatalf("%s has no tag check", file)
	return "", ""
}

// The tag check takes only one line that is a release number: grep would
// pass a multi-line input with one good line.
func TestTagCheck(t *testing.T) {
	for _, f := range []string{".github/workflows/release.yml", ".github/workflows/formula.yml"} {
		script, shell := tagCheck(t, f)
		if shell != "bash" {
			t.Errorf("%s: the tag check runs in %q, not bash", f, shell)
		}
		for tag, want := range map[string]bool{
			"v0.1.3": true, "v10.20.30": true,
			"0.1.3": false, "v0.1": false, "v0.1.3-rc1": false, "v0.1.3\nv0.1.4": false, "x\nv0.1.3": false, "v0.1.3\n": false, "": false,
		} {
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "TAG="+tag)
			err := cmd.Run()
			if (err == nil) != want {
				t.Errorf("%s: tag %q: passed %v, want %v", f, tag, err == nil, want)
			}
		}
	}
}
