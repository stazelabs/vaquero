package timefmt_test

import (
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/timefmt"
)

func TestDayLabel(t *testing.T) {
	now := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC) // a Wednesday
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-time.Hour), "today"},
		{now.Add(time.Hour), "today"}, // after now still reads today
		{time.Date(2026, 11, 24, 23, 59, 0, 0, time.UTC), "yesterday"},
		{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), "Fri"},
		{time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC), "Thu"},
		{time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC), "Nov 18, 2026"},
		// Days are UTC days, whatever zone the instants are in.
		{time.Date(2026, 11, 24, 20, 0, 0, 0, time.FixedZone("x", -5*3600)), "today"},
	} {
		if got := timefmt.DayLabel(tc.at, now); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.at, got, tc.want)
		}
	}
}
