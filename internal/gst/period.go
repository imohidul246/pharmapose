package gst

import (
	"fmt"
	"time"
)

// GST periods are calendar months. The key "YYYY-MM" (e.g. "2026-08") avoids
// the ambiguity of the GSTN "MMYYYY" code (e.g. "082026") while both are
// derived from a single source of truth: the document date.
func periodKey(t time.Time) string {
	return t.Format("2006-01")
}

// parsePeriod parses a "YYYY-MM" period key.
func parsePeriod(s string) (time.Time, error) {
	return time.Parse("2006-01", s)
}

// periodRange returns the [start, end) month boundaries for a period key.
func periodRange(key string) (time.Time, time.Time, error) {
	t, err := parsePeriod(key)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return t, t.AddDate(0, 1, 0), nil
}

// fiscalYearFor returns the GST financial year string ("2026-27") for a date.
func fiscalYearFor(t time.Time) string {
	y := t.Year()
	if t.Month() >= time.April {
		return fmt.Sprintf("%d-%02d", y, (y+1)%100)
	}
	return fmt.Sprintf("%d-%02d", y-1, y%100)
}

// gstnPeriodCode renders the GSTN return-period code "MMYYYY" (e.g. "082026").
func gstnPeriodCode(t time.Time) string {
	return fmt.Sprintf("%02d%d", int(t.Month()), t.Year())
}
