// Package booking computes private appointment availability. It grants no
// reservation, calendar-write, agreement, funding or software-work authority.
package booking

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // The restricted runtime image does not supply zoneinfo files.
)

// ErrUnavailable covers closed policy and unknown availability without exposing
// private calendar details in an error.
var ErrUnavailable = errors.New("booking availability is unavailable")

// Interval is a half-open interval of instants, [Start, End).
type Interval struct{ Start, End time.Time }

// Opening replaces the normal window on one explicitly opened Copenhagen date.
// Start and End are wall-clock offsets from midnight, with End at most 24 hours.
type Opening struct {
	Date       string
	Start, End time.Duration
}

// Policy has no business defaults. Pointer durations distinguish an explicit
// zero from a choice the operator has not made. Durations are whole minutes.
type Policy struct {
	Enabled                  bool
	SlotStep                 time.Duration
	WeeklyCapacity           time.Duration
	LeadTime                 *time.Duration
	BufferBefore             *time.Duration
	BufferAfter              *time.Duration
	CancellationNotice       *time.Duration
	CancellationTermsVersion string
	Openings                 []Opening
	ClosedDates              []string
}

// BusySnapshot contains no event title, location, attendee, UID or description.
// A provider must preserve recurrence/all-day/timezone semantics before marking
// the bounded coverage complete. Its adapter owns that verification.
type BusySnapshot struct {
	ObservedAt time.Time
	Coverage   Interval
	Complete   bool
	Busy       []Interval
}

// BusyProvider reads only operator-selected calendars. Implementations must
// return no partial result on error, and must not expose raw provider errors.
type BusyProvider interface {
	Busy(context.Context, Interval) (BusySnapshot, error)
}

// Input is built server-side. Commitments must include every active reservation
// and confirmed appointment for each whole Copenhagen week touched by Search,
// including those outside Search. Expiry and atomic enforcement belong to the
// durable reservation service, not this advisory calculation.
type Input struct {
	Now         time.Time
	Search      Interval
	Duration    time.Duration
	Calendar    BusySnapshot
	Commitments []Interval
	// Coverage attests that the database read included complete local weeks,
	// not merely appointments inside the visible search page.
	CommitmentsCoverage Interval
	CommitmentsComplete bool
}

// MaxSnapshotAge bounds advisory cache reuse. Confirmation still needs a new
// provider check; a recent snapshot never grants calendar-write authority.
const MaxSnapshotAge = 30 * time.Second

const maxIntervals = 4096

// Available returns candidate appointment intervals, never a reservation. The
// caller supplies the duration authorized for this meeting; it is not inferred
// from a product, an email address or the free-introduction entitlement.
func Available(p Policy, in Input) ([]Interval, error) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil || !validPolicy(p) || !validInput(p, in, loc) {
		return nil, ErrUnavailable
	}
	blocked := make([]Interval, 0, len(in.Calendar.Busy)+len(in.Commitments))
	blocked = append(blocked, in.Calendar.Busy...)
	for _, c := range in.Commitments {
		blocked = append(blocked, Interval{c.Start.Add(-*p.BufferBefore), c.End.Add(*p.BufferAfter)})
	}
	blocked = mergeIntervals(blocked)
	day := midnight(in.Search.Start.In(loc))
	var result []Interval
	for day.Before(in.Search.End) {
		start, end, open := window(p, day)
		if open {
			week := weekStart(day)
			used := committedTime(in.Commitments, week, week.AddDate(0, 0, 7))
			if used <= p.WeeklyCapacity-in.Duration {
				for at := start; !at.Add(in.Duration).After(end); at = at.Add(p.SlotStep) {
					candidate := Interval{at.UTC(), at.Add(in.Duration).UTC()}
					if candidate.Start.Before(in.Search.Start) || candidate.End.After(in.Search.End) || candidate.Start.Before(in.Now.Add(*p.LeadTime)) {
						continue
					}
					occupied := Interval{candidate.Start.Add(-*p.BufferBefore), candidate.End.Add(*p.BufferAfter)}
					if !conflicts(blocked, occupied) {
						result = append(result, candidate)
					}
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return result, nil
}

func wholeMinutes(d time.Duration, max time.Duration) bool {
	return d >= 0 && d <= max && d%time.Minute == 0
}

func validPolicy(p Policy) bool {
	if !p.Enabled || p.SlotStep <= 0 || !wholeMinutes(p.SlotStep, 24*time.Hour) || p.WeeklyCapacity <= 0 || !wholeMinutes(p.WeeklyCapacity, 7*24*time.Hour) || strings.TrimSpace(p.CancellationTermsVersion) == "" || len(p.CancellationTermsVersion) > 128 {
		return false
	}
	for _, d := range []*time.Duration{p.LeadTime, p.CancellationNotice} {
		if d == nil || !wholeMinutes(*d, 365*24*time.Hour) {
			return false
		}
	}
	for _, d := range []*time.Duration{p.BufferBefore, p.BufferAfter} {
		if d == nil || !wholeMinutes(*d, 24*time.Hour) {
			return false
		}
	}
	if len(p.Openings) > 366 || len(p.ClosedDates) > 366 {
		return false
	}
	seen := make(map[string]bool, len(p.Openings))
	for _, opening := range p.Openings {
		if !validDate(opening.Date) || seen[opening.Date] || !wholeMinutes(opening.Start, 24*time.Hour) || !wholeMinutes(opening.End, 24*time.Hour) || opening.Start >= opening.End {
			return false
		}
		seen[opening.Date] = true
	}
	for _, date := range p.ClosedDates {
		if !validDate(date) {
			return false
		}
	}
	return true
}

func validDate(s string) bool {
	d, err := time.Parse("2006-01-02", s)
	return err == nil && d.Format("2006-01-02") == s
}

func validInterval(i Interval) bool {
	return !i.Start.IsZero() && !i.End.IsZero() && i.Start.Before(i.End)
}

func validInput(p Policy, in Input, loc *time.Location) bool {
	c := in.Calendar
	if in.Now.IsZero() || !validInterval(in.Search) || in.Search.End.Sub(in.Search.Start) > 31*24*time.Hour || in.Duration <= 0 || !wholeMinutes(in.Duration, 24*time.Hour) || !c.Complete || c.ObservedAt.IsZero() || c.ObservedAt.After(in.Now) || in.Now.Sub(c.ObservedAt) > MaxSnapshotAge || !validInterval(c.Coverage) {
		return false
	}
	if c.Coverage.Start.After(in.Search.Start.Add(-*p.BufferBefore)) || c.Coverage.End.Before(in.Search.End.Add(*p.BufferAfter)) || len(c.Busy) > maxIntervals || len(in.Commitments) > maxIntervals {
		return false
	}
	// The buffer sum includes space occupied by an appointment just outside
	// the queried week as well as this candidate's own buffered interval.
	margin := *p.BufferBefore + *p.BufferAfter
	from := weekStart(midnight(in.Search.Start.In(loc))).Add(-margin)
	until := weekStart(midnight(in.Search.End.Add(-time.Nanosecond).In(loc))).AddDate(0, 0, 7).Add(margin)
	if !in.CommitmentsComplete || !validInterval(in.CommitmentsCoverage) || in.CommitmentsCoverage.Start.After(from) || in.CommitmentsCoverage.End.Before(until) {
		return false
	}
	for _, intervals := range [][]Interval{c.Busy, in.Commitments} {
		for _, interval := range intervals {
			if !validInterval(interval) {
				return false
			}
		}
	}
	return true
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func weekStart(day time.Time) time.Time {
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}

func window(p Policy, day time.Time) (time.Time, time.Time, bool) {
	date := day.Format("2006-01-02")
	for _, closed := range p.ClosedDates {
		if closed == date {
			return time.Time{}, time.Time{}, false
		}
	}
	start, end := 16*time.Hour+30*time.Minute, 19*time.Hour
	open := day.Weekday() >= time.Monday && day.Weekday() <= time.Friday
	for _, opening := range p.Openings {
		if opening.Date == date {
			start, end, open = opening.Start, opening.End, true
			break
		}
	}
	from, validFrom := wallTime(day, start)
	until, validUntil := wallTime(day, end)
	return from, until, open && validFrom && validUntil && from.Before(until)
}

// An opening boundary in a missing or repeated DST hour is ambiguous without
// an offset choice. Refuse that date instead of silently moving the boundary.
func wallTime(day time.Time, offset time.Duration) (time.Time, bool) {
	if offset == 24*time.Hour {
		return day.AddDate(0, 0, 1), true
	}
	hour, minute := int(offset/time.Hour), int(offset%time.Hour/time.Minute)
	t := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
	matches := func(v time.Time) bool {
		return v.Year() == day.Year() && v.YearDay() == day.YearDay() && v.Hour() == hour && v.Minute() == minute
	}
	return t, matches(t) && !matches(t.Add(-time.Hour)) && !matches(t.Add(time.Hour))
}

func committedTime(commitments []Interval, start, end time.Time) time.Duration {
	var used time.Duration
	for _, c := range commitments {
		from, until := c.Start, c.End
		if from.Before(start) {
			from = start
		}
		if until.After(end) {
			until = end
		}
		if from.Before(until) {
			used += until.Sub(from)
		}
	}
	return used
}

func mergeIntervals(intervals []Interval) []Interval {
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start.Before(intervals[j].Start) })
	merged := intervals[:0]
	for _, interval := range intervals {
		if len(merged) == 0 || interval.Start.After(merged[len(merged)-1].End) {
			merged = append(merged, interval)
		} else if interval.End.After(merged[len(merged)-1].End) {
			merged[len(merged)-1].End = interval.End
		}
	}
	return merged
}

func conflicts(blocked []Interval, candidate Interval) bool {
	i := sort.Search(len(blocked), func(i int) bool { return blocked[i].End.After(candidate.Start) })
	return i < len(blocked) && blocked[i].Start.Before(candidate.End)
}
