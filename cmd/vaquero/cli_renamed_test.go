package main

import (
	"sort"
	"strings"
	"testing"
)

// TestRenamedSpellings: every old spelling is a usage error naming the new one.
func TestRenamedSpellings(t *testing.T) {
	home := t.TempDir() // never the real saved connections
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"project", "create", "X", "Y"}, `"project create" is now "project new"`},
		{[]string{"status", "add", "VAQ", "X"}, `"status add" is now "status new"`},
		{[]string{"status", "rename", "VAQ", "A", "B"}, `"status rename" is now "status edit --name NEW"`},
		{[]string{"milestone", "add", "VAQ", "X"}, `"milestone add" is now "milestone new"`},
		{[]string{"tag", "rename", "VAQ", "a", "b"}, `"tag rename" is now "tag edit --name NEW"`},
		{[]string{"ref", "add", "VAQ-1", "design", "x"}, `"ref add" is now "ref new"`},
		{[]string{"ref", "remove", "VAQ-1", "id"}, `"ref remove" is now "ref delete"`},
		{[]string{"token", "create", "x"}, `"token create" is now "token new"`},
		{[]string{"token", "revoke", "x"}, `"token revoke" is now "token delete"`},
		{[]string{"attach", "VAQ-1", "f"}, `"attach" is now "attachment new"`},
		{[]string{"attachments", "VAQ-1"}, `"attachments" is now "attachment list"`},
		{[]string{"attachments", "move", "--to", "dir"}, `"attachments move" is now "attachment move"`},
		{[]string{"detach", "id"}, `"detach" is now "attachment delete"`},
		{[]string{"comment", "VAQ-1", "hi"}, `"comment" is now "comment new"`},
		{[]string{"link", "VAQ-1", "blocks", "VAQ-2"}, `"link" is now "ticket link"`},
		{[]string{"unlink", "VAQ-1", "VAQ-2"}, `"unlink" is now "ticket unlink"`},
		{[]string{"ticket", "move-to", "VAQ-1", "OTH"}, `"ticket move-to" is now "ticket transfer"`},
		{[]string{"release", "VAQ-1"}, `"release" is now "unclaim"`},
		{[]string{"profile", "list"}, `"profile list" is now "server list"`},
		{[]string{"profile"}, `"profile" is now "server"`},
		{[]string{"profile", "add", "a", "http://x", "t"}, `"profile add" is now "server new"`},
		{[]string{"profile", "remove", "a"}, `"profile remove" is now "server delete"`},
		{[]string{"ticket", "show", "VAQ-1", "--profile", "x"}, `"--profile" is now "--server"`},
		{[]string{"--profile=x", "projects"}, `"--profile" is now "--server"`},
	}
	for _, tc := range cases {
		code, out, errOut := cli(t, tc.args...)
		if code != 2 || out != "" || !strings.HasPrefix(errOut, "vaquero: ") || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: exit %d, out %q, err %q; want exit 2 naming %s", tc.args, code, out, errOut, tc.want)
		}
	}
}

// TestUsageListsEveryCommand walks the dispatch table.
func TestUsageListsEveryCommand(t *testing.T) {
	names := []string{"serve", "version", "help"}
	for name := range cliCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	var missing []string
	for _, n := range names {
		listed := false
		for line := range strings.SplitSeq(usage, "\n") {
			if f := strings.Fields(line); len(f) > 0 && f[0] == n {
				listed = true
			}
		}
		if !listed && n != "help" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the usage text lacks: %s", strings.Join(missing, ", "))
	}
	for _, sub := range []string{"compact", "transfer", "unclaim", "server"} {
		if !strings.Contains(usage, sub) {
			t.Errorf("the usage text never says %q", sub)
		}
	}
}
