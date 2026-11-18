package timefmt

import (
	"testing"
	"time"
)

func TestFormatIsUTCMillis(t *testing.T) {
	loc := time.FixedZone("X", -7*3600)
	in := time.Date(2026, 10, 8, 5, 4, 3, 7_000_000, loc)
	got := Format(in)
	if got != "2026-10-08T12:04:03.007Z" {
		t.Fatalf("got %q", got)
	}
	back, err := Parse(got)
	if err != nil || !back.Equal(in.Truncate(time.Millisecond)) {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

func TestFormatSortsLexically(t *testing.T) {
	a := Format(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	b := Format(time.Date(2026, 1, 1, 0, 0, 0, 1_000_000, time.UTC))
	if !(a < b) {
		t.Fatalf("%q should sort before %q", a, b)
	}
}

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                               "0 seconds",
		time.Second:                     "1 second",
		4 * time.Second:                 "4 seconds",
		time.Minute:                     "1 minute",
		12*time.Minute + 59*time.Second: "12 minutes",
		3*time.Hour + 30*time.Minute:    "3 hours",
		49 * time.Hour:                  "2 days",
		-5 * time.Second:                "0 seconds",
		1500 * time.Millisecond:         "1 second",
	} {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestOffsetAndClock(t *testing.T) {
	jul := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	ny, _ := time.LoadLocation("America/New_York")
	kol, _ := time.LoadLocation("Asia/Kolkata")
	for _, c := range []struct {
		at         time.Time
		loc        *time.Location
		off, clock string
	}{
		{jul, time.UTC, "UTC+00:00", "12:00"},
		{jul, nil, "UTC+00:00", "12:00"},
		{jul, kol, "UTC+05:30", "17:30"},
		{jul, ny, "UTC−04:00", "08:00"},
		{jan, ny, "UTC−05:00", "07:00"},
	} {
		if got := Offset(c.at, c.loc); got != c.off {
			t.Errorf("Offset = %q, want %q", got, c.off)
		}
		if got := Clock(c.at, c.loc); got != c.clock {
			t.Errorf("Clock = %q, want %q", got, c.clock)
		}
	}
}

func TestNull(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 30, 0, 0, time.UTC)
	got := Null(at)
	if !got.Valid || got.String != Format(at) {
		t.Errorf("Null = %+v, want valid %q", got, Format(at))
	}
}

func TestCalendarDays(t *testing.T) {
	at := time.Date(2026, 10, 7, 23, 30, 0, 0, time.FixedZone("x", -5*3600))
	for _, c := range []struct{ name, got, want string }{
		{"Day", Day("2026-10-07"), "Oct 7, 2026"},
		{"Day bad", Day("soon"), "soon"},
		{"Day empty", Day(""), ""},
		{"DayLong", DayLong("2026-10-07"), "October 7, 2026"},
		{"DayLong bad", DayLong("soon"), "soon"},
		{"DayOf is UTC", DayOf(at), "Oct 8, 2026"},
		{"DayLongOf is UTC", DayLongOf(at), "October 8, 2026"},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

// The database writes its own stamps (goose's tstamp is SQLite's
// datetime('now'): UTC, no zone). ParseStamp reads both that and a Format
// string, and both are UTC.
func TestParseStamp(t *testing.T) {
	want := time.Date(2026, 10, 21, 18, 10, 0, 0, time.UTC)
	for _, in := range []string{"2026-10-21 18:10:00", "2026-10-21T18:10:00.000Z"} {
		got, err := ParseStamp(in)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("ParseStamp(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseStamp("yesterday"); err == nil {
		t.Error("ParseStamp accepted a word")
	}
}
