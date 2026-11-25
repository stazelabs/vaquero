package board_test

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stazelabs/vaquero/internal/board"
)

// Review Focus 1: odd query values never panic and become "any" or a value
// that simply matches nothing.
func TestParseFilterOddInput(t *testing.T) {
	cases := map[string]board.Filter{
		"":                         {Priority: -1},
		"priority=9":               {Priority: -1},
		"priority=x":               {Priority: -1},
		"priority=-1":              {Priority: -1},
		"priority=0":               {Priority: 0},
		"priority=2&priority=3":    {Priority: 2},
		"mine=1&agents=yes":        {Mine: true, Agents: true, Priority: -1},
		"mine=0":                   {Priority: -1},
		"milestone=garbage":        {Milestone: "garbage", Priority: -1},
		"milestone=none":           {Milestone: "none", Priority: -1},
		"tag=%20UI%20":             {Tag: "UI", Priority: -1},
		"assignee=abc&unknown=zzz": {Assignee: "abc", Priority: -1},
	}
	for raw, want := range cases {
		q, _ := url.ParseQuery(raw)
		if got := board.ParseFilter(q); got != want {
			t.Errorf("%q → %+v, want %+v", raw, got, want)
		}
	}
	long := url.Values{"tag": {strings.Repeat("x", 500)}}
	if f := board.ParseFilter(long); len(f.Tag) > 100 {
		t.Errorf("tag kept at %d characters", len(f.Tag))
	}
}

func TestFilterRoundTrip(t *testing.T) {
	f := board.Filter{Mine: true, Tag: "ui", Priority: 2, Milestone: "none", Assignee: "a1"}
	if got := board.ParseFilter(f.Query()); got != f {
		t.Fatalf("%+v → %v → %+v", f, f.Query().Encode(), got)
	}
	if (board.Filter{Priority: -1}).Query().Encode() != "" {
		t.Fatal("empty filter has a query")
	}
}

func TestFilterWith(t *testing.T) {
	f := board.Filter{Priority: -1}.With("tag", "ui").With("priority", "1")
	if f.Tag != "ui" || f.Priority != 1 || !f.Active() {
		t.Fatalf("%+v", f)
	}
	f = f.With("tag", "").With("priority", "")
	if f.Active() {
		t.Fatalf("still active: %+v", f)
	}
	if !(board.Filter{Priority: -1}).With("mine", "1").Mine {
		t.Fatal("mine")
	}
}

// Filter values are cut to 100 characters, never mid-character.
func TestParseFilterClipsCharacters(t *testing.T) {
	for _, v := range []string{strings.Repeat("é", 101), strings.Repeat("🐄", 101), strings.Repeat("a", 99) + "éé"} {
		f := board.ParseFilter(url.Values{"tag": {v}})
		if n := len([]rune(f.Tag)); n != 100 || !utf8.ValidString(f.Tag) || !strings.HasPrefix(v, f.Tag) {
			t.Errorf("%q: %d characters, %q", v[:8], n, f.Tag)
		}
	}
}
