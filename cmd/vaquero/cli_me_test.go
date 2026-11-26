package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

// VAQ-149: vaquero me shows and edits the owner's profile.
func TestCLIMe(t *testing.T) {
	e := cliEnv(t)
	if out := mustCLI(t, "me"); !strings.Contains(out, "Name: Owner") || !strings.Contains(out, "Avatar: monogram OW") {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "me", "--name", "Kim Lee"); !strings.Contains(out, "Name: Kim Lee") || !strings.Contains(out, "Avatar: monogram KL") {
		t.Fatalf("%q", out)
	}
	png := writeFile(t, "me.png", tinyPNG)
	var p api.Profile
	if err := json.Unmarshal([]byte(mustCLI(t, "me", "--avatar", png, "--json")), &p); err != nil || p.Avatar == nil || p.Shown != "Kim Lee" {
		t.Fatalf("%v %+v", err, p)
	}
	if out := mustCLI(t, "me"); !strings.Contains(out, "Avatar: image") {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "me", "--remove-avatar"); !strings.Contains(out, "Avatar: monogram KL") {
		t.Fatalf("%q", out)
	}
	if _, err := e.Deps.R.GetActorAvatarEtag(t.Context(), e.Deps.Actor.ID); err == nil {
		t.Fatal("the avatar is still stored")
	}
	if out := mustCLI(t, "me", "--name", ""); !strings.Contains(out, "Name: Owner") {
		t.Fatalf("%q", out)
	}
	// Refusals: the server's words, exit 1; an agent is refused.
	if code, _, errOut := cli(t, "me", "--name", strings.Repeat("k", 81)); code != 1 || !strings.Contains(errOut, "Keep the name to 80 characters.") {
		t.Fatalf("%d %q", code, errOut)
	}
	txt := writeFile(t, "fake.png", []byte("not an image at all"))
	if code, _, errOut := cli(t, "me", "--avatar", txt); code != 1 || !strings.Contains(errOut, "Use a PNG, JPEG or WebP image.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "me", "--name", "Claude", "--agent", "claude"); code != 1 || !strings.Contains(errOut, "Only the owner edits the owner's profile.") {
		t.Fatalf("agent: %d %q", code, errOut)
	}
	for _, args := range [][]string{
		{"me", "--avatar", png, "--remove-avatar"},
		{"me", "extra"},
	} {
		if code, _, _ := cli(t, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}
