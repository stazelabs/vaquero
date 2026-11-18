package timefmt

import (
	"testing"
	"time"
)

func TestAgo(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		at   time.Time
		now  time.Time
		loc  *time.Location
		want string
	}{
		{"future", now.Add(time.Minute), now, time.UTC, "Just now"},
		{"59 seconds", now.Add(-59 * time.Second), now, time.UTC, "Just now"},
		{"1 minute", now.Add(-time.Minute), now, time.UTC, "1 minute ago"},
		{"2 minutes", now.Add(-2 * time.Minute), now, time.UTC, "2 minutes ago"},
		{"59 minutes", now.Add(-59 * time.Minute), now, time.UTC, "59 minutes ago"},
		{"1 hour", now.Add(-time.Hour), now, time.UTC, "1 hour ago"},
		{"2 hours", now.Add(-2 * time.Hour), now, time.UTC, "2 hours ago"},
		{"23 hours", time.Date(2026, 10, 28, 23, 30, 0, 0, time.UTC).Add(-23 * time.Hour), time.Date(2026, 10, 28, 23, 30, 0, 0, time.UTC), time.UTC, "23 hours ago"},
		{"yesterday, 13 hours", now.Add(-13 * time.Hour), now, time.UTC, "Yesterday"},
		// Berlin is UTC+1 on these dates (CEST ended Oct 25).
		{"yesterday across midnight in Berlin", time.Date(2026, 10, 27, 22, 30, 0, 0, time.UTC), time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC), berlin, "Yesterday"},
		{"same Berlin day, different UTC day", time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC), time.Date(2026, 10, 28, 0, 40, 0, 0, time.UTC), berlin, "1 hour ago"},
		{"same UTC day, different Berlin day", time.Date(2026, 10, 27, 22, 30, 0, 0, time.UTC), time.Date(2026, 10, 27, 23, 40, 0, 0, time.UTC), berlin, "Yesterday"},
		{"4 days", now.AddDate(0, 0, -4), now, time.UTC, "4 days ago"},
		{"7 days", now.AddDate(0, 0, -7), now, time.UTC, "7 days ago"},
		{"8 days", now.AddDate(0, 0, -8), now, time.UTC, "Oct 20"},
		{"last year", time.Date(2025, 12, 30, 9, 0, 0, 0, time.UTC), now, time.UTC, "Dec 30, 2025"},
		{"nil zone", now.Add(-3 * time.Hour), now, nil, "3 hours ago"},
	} {
		if got := Ago(c.at, c.now, c.loc); got != c.want {
			t.Errorf("%s: %q want %q", c.name, got, c.want)
		}
	}
}
