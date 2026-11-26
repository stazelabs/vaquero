// Package timefmt is the only place instants are formatted for storage, and
// where spans of time are worded for people.
//
// Storage: Format writes fixed-width UTC with milliseconds, so stored strings
// compare lexically; Null is Format for a nullable column. Display* words an
// instant in a person's zone.
//
// Calendar days (a due date, a follow-up day, a report day) are dates, not
// instants: Day and DayLong word a YYYY-MM-DD string ("Oct 7, 2026", "October
// 7, 2026") and return it unchanged when it isn't one. DayOf and DayLongOf do
// the same for an external fact held as a time.Time (a certificate's expiry,
// a domain's registration), in UTC, which is how its source dates it.
package timefmt

import (
	"database/sql"
	"strconv"
	"time"
)

// Layout is RFC3339 with exactly three fractional digits.
const Layout = "2006-01-02T15:04:05.000Z07:00"

// Format renders t in UTC using Layout.
func Format(t time.Time) string { return t.UTC().Format(Layout) }

// Null is Format as a valid sql.NullString, for a nullable instant column.
func Null(t time.Time) sql.NullString { return sql.NullString{String: Format(t), Valid: true} }

// Parse reads a string produced by Format.
func Parse(s string) (time.Time, error) { return time.Parse(Layout, s) }

// dbStamp is SQLite's datetime('now'), the default the database writes into
// goose_db_version.tstamp: UTC, with no zone.
const dbStamp = "2006-01-02 15:04:05"

// ParseStamp reads an instant the database wrote itself: a Format string or
// SQLite's datetime('now') form ("2026-10-21 18:10:00"). Both are UTC.
func ParseStamp(s string) (time.Time, error) {
	if t, err := Parse(s); err == nil {
		return t, nil
	}
	return time.Parse(dbStamp, s)
}

// FileStamp is an instant for a file name, in UTC to the second:
// "20261120-120000".
func FileStamp(t time.Time) string { return t.UTC().Format("20060102-150405") }

// Duration renders a span for people, rounded down to its largest unit:
// "4 seconds", "1 minute", "3 hours", "2 days". Negative spans read as
// "0 seconds".
func Duration(d time.Duration) string {
	unit := func(n int64, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.FormatInt(n, 10) + " " + one + "s"
	}
	switch {
	case d >= 48*time.Hour:
		return unit(int64(d/(24*time.Hour)), "day")
	case d >= time.Hour:
		return unit(int64(d/time.Hour), "hour")
	case d >= time.Minute:
		return unit(int64(d/time.Minute), "minute")
	case d > 0:
		return unit(int64(d/time.Second), "second")
	}
	return "0 seconds"
}

func zone(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}

// Display renders an instant for a person in loc, with the zone's
// abbreviation: "Oct 13, 2026, 14:30 CEST". A nil loc is UTC.
func Display(t time.Time, loc *time.Location) string {
	return t.In(zone(loc)).Format("Jan 2, 2006, 15:04 MST")
}

// DisplayDate is the date of t in loc: "Oct 13, 2026". It carries no zone.
func DisplayDate(t time.Time, loc *time.Location) string {
	return t.In(zone(loc)).Format("Jan 2, 2006")
}

// DisplayClock is the time of day in loc: "14:30 CEST".
func DisplayClock(t time.Time, loc *time.Location) string {
	return t.In(zone(loc)).Format("15:04 MST")
}

// DisplayUTC is the machine-friendly form shown on hover:
// "2026-10-13 12:30 UTC".
func DisplayUTC(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") + " UTC" }

// DisplayLong is for a sentence in an email: "13 October 2026 at 14:30 CEST".
func DisplayLong(t time.Time, loc *time.Location) string {
	return t.In(zone(loc)).Format("2 January 2006 at 15:04 MST")
}

// Offset is loc's UTC offset at t, for choosing a time zone:
// "UTC+05:30", "UTC−08:00" (a true minus sign). A nil loc is UTC.
func Offset(t time.Time, loc *time.Location) string {
	_, secs := t.In(zone(loc)).Zone()
	sign := "+"
	if secs < 0 {
		sign, secs = "−", -secs
	}
	m := secs / 60
	return "UTC" + sign + pad2(m/60) + ":" + pad2(m%60)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// Clock is the time of day in loc without a zone name: "14:30". Use it
// beside an offset; DisplayClock carries the abbreviation.
func Clock(t time.Time, loc *time.Location) string {
	return t.In(zone(loc)).Format("15:04")
}

// Day words a YYYY-MM-DD calendar day as "Oct 7, 2026", or returns s when it
// isn't one. A day names no instant, so it carries no zone.
func Day(s string) string { return dayWords(s, "Jan 2, 2006") }

// DayLong is Day with the month spelled out: "October 7, 2026".
func DayLong(s string) string { return dayWords(s, "January 2, 2006") }

// DayOf is the UTC date of t as "Oct 7, 2026". Use it for an external fact
// dated by its source, such as a certificate's expiry.
func DayOf(t time.Time) string { return t.UTC().Format("Jan 2, 2006") }

// DayLongOf is DayOf with the month spelled out: "October 7, 2026".
func DayLongOf(t time.Time) string { return t.UTC().Format("January 2, 2006") }

func dayWords(s, layout string) string {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return s
	}
	return t.Format(layout)
}

// Ago words how long before now an instant was, for a feed: "Just now",
// "5 minutes ago", "3 hours ago", "Yesterday", "4 days ago". Days are
// calendar days in loc, so a line from 23:50 reads Yesterday at 00:10. Past
// 7 days it falls back to a short date ("Oct 21", with the year when it
// isn't now's year). An instant after now reads Just now. A nil loc is UTC.
func Ago(t, now time.Time, loc *time.Location) string {
	loc = zone(loc)
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "Just now"
	case d < time.Hour:
		return ago(int(d/time.Minute), "minute")
	}
	ty, tm, td := t.In(loc).Date()
	ny, nm, nd := now.In(loc).Date()
	days := int(time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	switch {
	case days <= 0:
		return ago(int(d/time.Hour), "hour")
	case days == 1:
		return "Yesterday"
	case days <= 7:
		return ago(days, "day")
	case ty == ny:
		return t.In(loc).Format("Jan 2")
	}
	return t.In(loc).Format("Jan 2, 2006")
}

func ago(n int, unit string) string {
	if n != 1 {
		unit += "s"
	}
	return strconv.Itoa(n) + " " + unit + " ago"
}

// DayLabel words the UTC day of at relative to now's, for a list of recent
// work: "today" (also for an instant after now), "yesterday", the weekday
// ("Mon") within the last 6 days, else the date ("Nov 18, 2026").
func DayLabel(at, now time.Time) string {
	day := func(t time.Time) time.Time {
		y, m, d := t.UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	switch back := int(day(now).Sub(day(at)).Hours() / 24); {
	case back <= 0:
		return "today"
	case back == 1:
		return "yesterday"
	case back < 7:
		return at.UTC().Weekday().String()[:3]
	default:
		return DayOf(at)
	}
}
