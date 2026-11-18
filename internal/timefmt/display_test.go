package timefmt

import (
	"testing"
	"time"
)

func TestDisplayInZone(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	la, _ := time.LoadLocation("America/Los_Angeles")
	at := time.Date(2026, 10, 13, 12, 30, 0, 0, time.UTC)
	late := time.Date(2026, 10, 13, 23, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"berlin", Display(at, berlin), "Oct 13, 2026, 14:30 CEST"},
		{"la", Display(at, la), "Oct 13, 2026, 05:30 PDT"},
		{"utc", Display(at, time.UTC), "Oct 13, 2026, 12:30 UTC"},
		{"nil zone is UTC", Display(at, nil), "Oct 13, 2026, 12:30 UTC"},
		{"berlin next day", DisplayDate(late, berlin), "Oct 14, 2026"},
		{"la same day", DisplayDate(late, la), "Oct 13, 2026"},
		{"clock", DisplayClock(at, berlin), "14:30 CEST"},
		{"utc form", DisplayUTC(at), "2026-10-13 12:30 UTC"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestDisplayLongNamesTheZone(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	at := time.Date(2026, 10, 13, 12, 30, 0, 0, time.UTC)
	if got, want := DisplayLong(at, berlin), "13 October 2026 at 14:30 CEST"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
