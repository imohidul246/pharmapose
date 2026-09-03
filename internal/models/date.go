package models

import (
	"database/sql/driver"
	"fmt"
	"time"
)

const dateFormat = "2006-01-02"

// Date is a calendar-date value that marshals to/from JSON as "YYYY-MM-DD"
// and scans cleanly from PostgreSQL DATE columns via pgx.
type Date struct {
	time.Time
}

func NewDate(t time.Time) Date {
	tt := t.UTC()
	return Date{Time: time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses "YYYY-MM-DD".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateFormat, s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return Date{Time: t}, nil
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte(`""`), nil
	}
	return []byte(`"` + d.Format(dateFormat) + `"`), nil
}

func (d *Date) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == `null` || s == `""` {
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	t, err := ParseDate(s)
	if err != nil {
		return err
	}
	d.Time = t.Time
	return nil
}

func (d Date) String() string { return d.Format(dateFormat) }

func (d Date) Value() (driver.Value, error) { return d.Format(dateFormat), nil }

// Scan implements the sql.Scanner interface so that Date can be read from
// a PostgreSQL DATE column (which pgx sends as time.Time in binary format).
func (d *Date) Scan(src interface{}) error {
	if src == nil {
		d.Time = time.Time{}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		d.Time = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
		return nil
	case string:
		t, err := time.Parse(dateFormat, v)
		if err != nil {
			return fmt.Errorf("scan date: %w", err)
		}
		d.Time = t
		return nil
	default:
		return fmt.Errorf("scan date: unsupported type %T", src)
	}
}
