// Package cron reads the five fields of a cron expression, minute, hour,
// day of the month, month and day of the week, and tells the times they
// match.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is the times a cron expression matches.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	// domAny and dowAny are whether the day fields are *, as a day then
	// matches by the other alone; when both are restricted, a day matches
	// by either, as cron has it.
	domAny, dowAny bool
}

type field struct {
	name     string
	min, max int
	names    []string
}

var fields = [5]field{
	{name: "minute", min: 0, max: 59},
	{name: "hour", min: 0, max: 23},
	{name: "day of the month", min: 1, max: 31},
	{name: "month", min: 1, max: 12, names: []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}},
	// 7 is Sunday too, as 0 is.
	{name: "day of the week", min: 0, max: 7, names: []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}},
}

// Parse reads expr, five fields apart by white space. Each field is *, a
// value, a range a-b, either with a step as */n or a-b/n, or a list of them
// apart by commas. A month and a day of the week can be named by the first
// three letters of their names in English.
func Parse(expr string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != len(fields) {
		return nil, fmt.Errorf("a cron expression has 5 fields, minute hour day month weekday, not %d: %q", len(parts), expr)
	}
	var sets [5]uint64
	for i, part := range parts {
		set, err := fields[i].parse(part)
		if err != nil {
			return nil, fmt.Errorf("%s of %q: %w", fields[i].name, expr, err)
		}
		sets[i] = set
	}
	if sets[4]&(1<<7) != 0 {
		sets[4] |= 1
	}
	return &Schedule{
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domAny: parts[2] == "*", dowAny: parts[4] == "*",
	}, nil
}

func (f field) parse(s string) (uint64, error) {
	var set uint64
	for item := range strings.SplitSeq(s, ",") {
		lo, hi, step, err := f.parseItem(item)
		if err != nil {
			return 0, err
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << v
		}
	}
	return set, nil
}

func (f field) parseItem(item string) (lo, hi, step int, err error) {
	rng, stepText, stepped := strings.Cut(item, "/")
	step = 1
	if stepped {
		if step, err = strconv.Atoi(stepText); err != nil || step < 1 {
			return 0, 0, 0, fmt.Errorf("the step of %q is not a whole number above 0", item)
		}
	}
	switch {
	case rng == "*":
		lo, hi = f.min, f.max
	case strings.Contains(rng, "-"):
		from, to, _ := strings.Cut(rng, "-")
		if lo, err = f.value(from); err != nil {
			return 0, 0, 0, err
		}
		if hi, err = f.value(to); err != nil {
			return 0, 0, 0, err
		}
		if lo > hi {
			return 0, 0, 0, fmt.Errorf("the range %q runs backwards", rng)
		}
	default:
		if lo, err = f.value(rng); err != nil {
			return 0, 0, 0, err
		}
		hi = lo
		if stepped {
			hi = f.max
		}
	}
	return lo, hi, step, nil
}

func (f field) value(s string) (int, error) {
	for i, name := range f.names {
		if strings.EqualFold(s, name) {
			return i + f.min, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a %s", s, f.name)
	}
	if v < f.min || v > f.max {
		return 0, fmt.Errorf("%d is not a %s, which is from %d to %d", v, f.name, f.min, f.max)
	}
	return v, nil
}

// maxSearch bounds how far Next looks, past which a schedule such as 30 2 *
// that never comes has no next time.
const maxSearch = 5 * 366 * 24 * time.Hour

// Next returns the first time after t that s matches, to the minute, in the
// location of t, and false when there is none in the five years after t.
func (s *Schedule) Next(t time.Time) (time.Time, bool) {
	next := t.Truncate(time.Minute).Add(time.Minute)
	end := t.Add(maxSearch)
	for next.Before(end) {
		switch {
		case !has(s.month, int(next.Month())):
			next = time.Date(next.Year(), next.Month()+1, 1, 0, 0, 0, 0, next.Location())
		case !s.dayMatches(next):
			next = time.Date(next.Year(), next.Month(), next.Day()+1, 0, 0, 0, 0, next.Location())
		case !has(s.hour, next.Hour()):
			next = time.Date(next.Year(), next.Month(), next.Day(), next.Hour()+1, 0, 0, 0, next.Location())
		case !has(s.minute, next.Minute()):
			next = next.Add(time.Minute)
		default:
			return next, true
		}
	}
	return time.Time{}, false
}

func (s *Schedule) dayMatches(t time.Time) bool {
	dom := has(s.dom, t.Day())
	dow := has(s.dow, int(t.Weekday()))
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	default:
		return dom || dow
	}
}

func has(set uint64, v int) bool {
	return set&(1<<v) != 0
}
