package cron

import (
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNext(t *testing.T) {
	tests := []struct {
		expr, from string
		want       []string
	}{
		{"* * * * *", "2026-11-26 10:15:30", []string{"2026-11-26 10:16:00", "2026-11-26 10:17:00"}},
		{"*/5 * * * *", "2026-11-26 10:15:00", []string{"2026-11-26 10:20:00", "2026-11-26 10:25:00"}},
		{"0 * * * *", "2026-11-26 10:15:00", []string{"2026-11-26 11:00:00", "2026-11-26 12:00:00"}},
		{"30 2 * * *", "2026-11-26 10:15:00", []string{"2026-11-27 02:30:00", "2026-11-28 02:30:00"}},
		{"0 9 * * mon-fri", "2026-11-27 10:00:00", []string{"2026-11-30 09:00:00", "2026-12-01 09:00:00"}},
		{"0 0 1 jan *", "2026-11-26 10:00:00", []string{"2027-01-01 00:00:00", "2028-01-01 00:00:00"}},
		{"0 0 * * 7", "2026-11-26 10:00:00", []string{"2026-11-29 00:00:00", "2026-12-06 00:00:00"}},
		{"0 0 29 2 *", "2026-11-26 10:00:00", []string{"2028-02-29 00:00:00", "2032-02-29 00:00:00"}},
		{"0 0 31 * *", "2026-11-26 10:00:00", []string{"2026-12-31 00:00:00", "2027-01-31 00:00:00"}},
		{"15,45 8-10/2 * * *", "2026-11-26 08:20:00", []string{"2026-11-26 08:45:00", "2026-11-26 10:15:00", "2026-11-26 10:45:00", "2026-11-27 08:15:00"}},
		// Both days restricted: a day matches by either, the 1st or a Monday.
		{"0 0 1 * mon", "2026-11-26 10:00:00", []string{"2026-11-30 00:00:00", "2026-12-01 00:00:00", "2026-12-07 00:00:00"}},
		{"0 12 5/10 * *", "2026-11-26 10:00:00", []string{"2026-12-05 12:00:00", "2026-12-15 12:00:00", "2026-12-25 12:00:00", "2027-01-05 12:00:00"}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			s, err := Parse(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			from := at(tt.from)
			for _, w := range tt.want {
				next, ok := s.Next(from)
				if !ok || !next.Equal(at(w)) {
					t.Fatalf("Next(%s) = %s, %v; want %s", from, next, ok, w)
				}
				from = next
			}
		})
	}
}

func TestNext_Never(t *testing.T) {
	s, err := Parse("0 0 30 2 *")
	if err != nil {
		t.Fatal(err)
	}
	if next, ok := s.Next(at("2026-11-26 10:00:00")); ok {
		t.Errorf("Next = %s, want none for February 30", next)
	}
}

func TestNext_InTheLocationOfTheTime(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*60*60)
	s, err := Parse("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	next, ok := s.Next(time.Date(2026, 11, 26, 10, 0, 0, 0, tokyo))
	if want := time.Date(2026, 11, 27, 9, 0, 0, 0, tokyo); !ok || !next.Equal(want) {
		t.Errorf("Next = %s, want %s", next, want)
	}
}

func TestParse_Refuses(t *testing.T) {
	tests := []struct{ expr, want string }{
		{"* * * *", "5 fields"},
		{"* * * * * *", "5 fields"},
		{"60 * * * *", "minute of"},
		{"* 24 * * *", "hour of"},
		{"* * 0 * *", "day of the month of"},
		{"* * * 13 *", "month of"},
		{"* * * * 8", "day of the week of"},
		{"*/0 * * * *", "not a whole number above 0"},
		{"5-1 * * * *", "runs backwards"},
		{"x * * * *", `"x" is not a minute`},
		{"* * * foo *", `"foo" is not a month`},
		{"1,,2 * * * *", `"" is not a minute`},
	}
	for _, tt := range tests {
		if _, err := Parse(tt.expr); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Parse(%q) = %v, want an error saying %q", tt.expr, err, tt.want)
		}
	}
}
