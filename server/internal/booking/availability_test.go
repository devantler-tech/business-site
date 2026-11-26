package booking

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// Removing the default-off or explicit-policy checks would expose slots before
// the operator has chosen the business terms. Zero is valid only when supplied.
func TestAvailabilityRequiresExplicitPolicy(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	for _, tc := range []struct {
		name string
		edit func(*Policy)
	}{
		{"disabled", func(p *Policy) { p.Enabled = false }},
		{"missing horizon", func(p *Policy) { p.HorizonDays = 0 }},
		{"horizon exceeds the approved window", func(p *Policy) { p.HorizonDays = 31 }},
		{"missing lead time", func(p *Policy) { p.LeadTime = nil }},
		{"missing before buffer", func(p *Policy) { p.BufferBefore = nil }},
		{"missing after buffer", func(p *Policy) { p.BufferAfter = nil }},
		{"missing cancellation notice", func(p *Policy) { p.CancellationNotice = nil }},
		{"missing cancellation terms", func(p *Policy) { p.CancellationTermsVersion = "" }},
		{"missing weekly capacity", func(p *Policy) { p.WeeklyCapacity = 0 }},
		{"negative lead time", func(p *Policy) { p.LeadTime = duration(-time.Minute) }},
		{"negative buffer", func(p *Policy) { p.BufferAfter = duration(-time.Minute) }},
		{"zero slot step", func(p *Policy) { p.SlotStep = 0 }},
		{"fractional minute slot step", func(p *Policy) { p.SlotStep = time.Second }},
		{"invalid opening", func(p *Policy) {
			p.Openings = []Opening{{Date: "2026-02-30", Start: 9 * time.Hour, End: 17 * time.Hour}}
		}},
		{"duplicate opening", func(p *Policy) {
			p.Openings = []Opening{{Date: "2026-11-16", Start: 9 * time.Hour, End: 17 * time.Hour}, {Date: "2026-11-16", Start: 10 * time.Hour, End: 18 * time.Hour}}
		}},
		{"invalid holiday", func(p *Policy) { p.ClosedDates = []string{"2026-13-01"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := p
			tc.edit(&candidate)
			got, err := Available(candidate, in)
			if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
				t.Fatalf("unconfigured policy returned %d slots, err=%v", len(got), err)
			}
		})
	}
}

// The approved window is Copenhagen wall time; a UTC-based implementation is
// wrong for one half of the year and can offer a session beyond closing time.
func TestAvailabilityCopenhagenWindows(t *testing.T) {
	for _, tc := range []struct {
		day, first, last string
	}{
		{"2026-03-27", "2026-03-27T15:30:00Z", "2026-03-27T17:00:00Z"},
		{"2026-03-30", "2026-03-30T14:30:00Z", "2026-03-30T16:00:00Z"},
		{"2026-10-23", "2026-10-23T14:30:00Z", "2026-10-23T16:00:00Z"},
		{"2026-10-26", "2026-10-26T15:30:00Z", "2026-10-26T17:00:00Z"},
	} {
		t.Run(tc.day, func(t *testing.T) {
			p, in := fixture(t, tc.day)
			in.Duration = time.Hour
			got, err := Available(p, in)
			if err != nil || len(got) != 4 {
				t.Fatalf("want four one-hour slots, got %d, err=%v", len(got), err)
			}
			if !got[0].Start.Equal(instant(tc.first)) || !got[3].Start.Equal(instant(tc.last)) || !got[3].End.Equal(instant(tc.last).Add(time.Hour)) {
				t.Fatalf("wrong UTC bounds: %v", got)
			}
		})
	}
}

func TestAvailabilityRequiresExplicitDateOpening(t *testing.T) {
	p, in := fixture(t, "2026-11-21") // Saturday, even a fully free calendar is closed.
	got, err := Available(p, in)
	if err != nil || len(got) != 0 {
		t.Fatalf("weekend opened without operator choice: %v %v", got, err)
	}
	p.Openings = []Opening{{Date: "2026-11-21", Start: 9 * time.Hour, End: 12 * time.Hour}}
	in.Duration = 2 * time.Hour
	got, err = Available(p, in)
	want := []Interval{
		{instant("2026-11-21T08:00:00Z"), instant("2026-11-21T10:00:00Z")},
		{instant("2026-11-21T08:30:00Z"), instant("2026-11-21T10:30:00Z")},
		{instant("2026-11-21T09:00:00Z"), instant("2026-11-21T11:00:00Z")},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit opening: got %v err=%v", got, err)
	}
	p.ClosedDates = []string{"2026-11-21"}
	got, err = Available(p, in)
	if err != nil || len(got) != 0 {
		t.Fatalf("holiday did not override opening: %v %v", got, err)
	}
}

func TestAvailabilityBusyBuffersAndHalfOpenBoundaries(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	in.Calendar.Busy = []Interval{{instant("2026-11-16T16:00:00Z"), instant("2026-11-16T17:00:00Z")}}
	got, err := Available(p, in)
	want := []Interval{
		{instant("2026-11-16T15:30:00Z"), instant("2026-11-16T16:00:00Z")},
		{instant("2026-11-16T17:00:00Z"), instant("2026-11-16T17:30:00Z")},
		{instant("2026-11-16T17:30:00Z"), instant("2026-11-16T18:00:00Z")},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("half-open boundaries: %v %v", got, err)
	}
	p.BufferBefore = duration(10 * time.Minute)
	p.BufferAfter = duration(10 * time.Minute)
	got, err = Available(p, in)
	if err != nil || len(got) != 1 || !got[0].Start.Equal(instant("2026-11-16T17:30:00Z")) {
		t.Fatalf("buffered conflicts: %v %v", got, err)
	}
}

func TestAvailabilityLeadTimeAndCommitments(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	in.Now = instant("2026-11-16T15:00:00Z")
	in.Calendar.ObservedAt = in.Now
	p.LeadTime = duration(time.Hour)
	in.Commitments = []Interval{{instant("2026-11-16T16:30:00Z"), instant("2026-11-16T17:30:00Z")}}
	got, err := Available(p, in)
	want := []Interval{
		{instant("2026-11-16T16:00:00Z"), instant("2026-11-16T16:30:00Z")},
		{instant("2026-11-16T17:30:00Z"), instant("2026-11-16T18:00:00Z")},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("lead time or durable commitments ignored: %v %v", got, err)
	}
}

func TestAvailabilityWeeklyCapacityUsesCopenhagenMonday(t *testing.T) {
	p, in := fixture(t, "2026-11-16") // Monday in Copenhagen starts Sunday 23:00 UTC.
	p.WeeklyCapacity = time.Hour
	in.Commitments = []Interval{
		{instant("2026-11-15T21:00:00Z"), instant("2026-11-15T22:00:00Z")}, // previous week
		{instant("2026-11-15T22:30:00Z"), instant("2026-11-15T23:30:00Z")}, // half an hour this week
	}
	got, err := Available(p, in)
	if err != nil || len(got) != 5 {
		t.Fatalf("week clipping wrong: %v %v", got, err)
	}
	in.Commitments = append(in.Commitments, Interval{instant("2026-11-17T08:00:00Z"), instant("2026-11-17T08:01:00Z")})
	got, err = Available(p, in)
	if err != nil || len(got) != 0 {
		t.Fatalf("weekly capacity exceeded: %v %v", got, err)
	}
}

func TestAvailabilityRejectsUnknownCalendar(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	for _, tc := range []struct {
		name string
		edit func(*Input)
	}{
		{"incomplete", func(in *Input) { in.Calendar.Complete = false }},
		{"stale", func(in *Input) { in.Calendar.ObservedAt = in.Now.Add(-31 * time.Second) }},
		{"future", func(in *Input) { in.Calendar.ObservedAt = in.Now.Add(time.Second) }},
		{"missing timestamp", func(in *Input) { in.Calendar.ObservedAt = time.Time{} }},
		{"partial coverage", func(in *Input) { in.Calendar.Coverage.End = in.Search.End.Add(-time.Second) }},
		{"buffer coverage", func(in *Input) { in.Calendar.Coverage.Start = in.Search.Start; in.Duration = time.Hour }},
		{"invalid busy interval", func(in *Input) { in.Calendar.Busy = []Interval{{Start: in.Now, End: in.Now}} }},
		{"invalid commitment", func(in *Input) { in.Commitments = []Interval{{Start: in.Now, End: in.Now.Add(-time.Hour)}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := in
			tc.edit(&candidate)
			policy := p
			if tc.name == "buffer coverage" {
				policy.BufferBefore = duration(time.Minute)
			}
			got, err := Available(policy, candidate)
			if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
				t.Fatalf("untrusted calendar produced slots: %v %v", got, err)
			}
		})
	}
}

func TestAvailabilityRejectsInvalidSearch(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	for _, edit := range []func(*Input){
		func(in *Input) { in.Duration = 0 },
		func(in *Input) { in.Duration = time.Second },
		func(in *Input) { in.Search.End = in.Search.Start },
		func(in *Input) {
			in.Search.End = in.Search.Start.Add(32 * 24 * time.Hour)
			in.Calendar.Coverage.End = in.Search.End.Add(24 * time.Hour)
			in.CommitmentsCoverage.End = in.Search.End.AddDate(0, 0, 14)
		},
		func(in *Input) { in.Now = time.Time{} },
	} {
		candidate := in
		edit(&candidate)
		got, err := Available(p, candidate)
		if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
			t.Fatalf("invalid input produced %d slots, err=%v", len(got), err)
		}
	}
}

func TestAvailabilityRejectsPartialWeeklyCommitments(t *testing.T) {
	p, in := fixture(t, "2026-11-18")
	for _, tc := range []struct {
		name string
		edit func(*Input)
	}{
		{"incomplete", func(in *Input) { in.CommitmentsComplete = false }},
		{"search only", func(in *Input) { in.CommitmentsCoverage = in.Search }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := in
			tc.edit(&candidate)
			got, err := Available(p, candidate)
			if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
				t.Fatalf("partial weekly schedule produced availability: %v %v", got, err)
			}
		})
	}
}

func TestAvailabilityExplicitOpeningsAcrossDST(t *testing.T) {
	for _, tc := range []struct {
		day, first, last string
		count            int
	}{
		{"2026-03-29", "2026-03-29T00:00:00Z", "2026-03-29T01:30:00Z", 4},
		{"2026-10-25", "2026-10-24T23:00:00Z", "2026-10-25T02:30:00Z", 8},
	} {
		t.Run(tc.day, func(t *testing.T) {
			p, in := fixture(t, tc.day)
			p.Openings = []Opening{{Date: tc.day, Start: time.Hour, End: 4 * time.Hour}}
			got, err := Available(p, in)
			if err != nil || len(got) != tc.count || !got[0].Start.Equal(instant(tc.first)) || !got[len(got)-1].Start.Equal(instant(tc.last)) {
				t.Fatalf("wrong DST instants: %v %v", got, err)
			}
			p.Openings[0].Start = 2*time.Hour + 30*time.Minute
			got, err = Available(p, in)
			if err != nil || len(got) != 0 {
				t.Fatalf("ambiguous/missing wall-clock opening shifted: %v %v", got, err)
			}
		})
	}
}

func TestAvailabilityBoundsBookingHorizonInCopenhagen(t *testing.T) {
	for _, tc := range []struct {
		day, now, last string
	}{
		{"2026-03-30", "2026-02-28T16:30:00+01:00", "2026-03-30T16:30:00+02:00"},
		{"2026-10-26", "2026-09-26T16:30:00+02:00", "2026-10-26T16:30:00+01:00"},
	} {
		t.Run(tc.day, func(t *testing.T) {
			p, in := fixture(t, tc.day)
			in.Now = instant(tc.now)
			in.Calendar.ObservedAt = in.Now
			got, err := Available(p, in)
			if err != nil || len(got) != 1 || !got[0].Start.Equal(instant(tc.last)) {
				t.Fatalf("want only the boundary slot %s, got %v, err=%v", tc.last, got, err)
			}
		})
	}
}

func TestAvailabilityAllDayAndUnsortedBusyIntervals(t *testing.T) {
	p, in := fixture(t, "2026-11-16")
	in.Calendar.Busy = []Interval{in.Search}
	got, err := Available(p, in)
	if err != nil || len(got) != 0 {
		t.Fatalf("all-day busy ignored: %v %v", got, err)
	}
	in.Calendar.Busy = []Interval{
		{instant("2026-11-16T17:00:00Z"), instant("2026-11-16T18:00:00Z")},
		{instant("2026-11-16T16:00:00Z"), instant("2026-11-16T16:30:00Z")},
		{instant("2026-11-16T16:15:00Z"), instant("2026-11-16T17:00:00Z")},
	}
	before := append([]Interval(nil), in.Calendar.Busy...)
	got, err = Available(p, in)
	if err != nil || len(got) != 1 || !got[0].Start.Equal(instant("2026-11-16T15:30:00Z")) || !reflect.DeepEqual(in.Calendar.Busy, before) {
		t.Fatalf("busy union or caller input changed: %v %v", got, err)
	}
}

func fixture(t *testing.T, day string) (Policy, Input) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Fatal(err)
	}
	start, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		t.Fatal(err)
	}
	search := Interval{start, start.AddDate(0, 0, 1)}
	now := start.Add(-24 * time.Hour)
	p := Policy{
		Enabled: true, HorizonDays: 30, SlotStep: 30 * time.Minute, WeeklyCapacity: 10 * time.Hour,
		LeadTime: duration(0), BufferBefore: duration(0), BufferAfter: duration(0),
		CancellationNotice: duration(0), CancellationTermsVersion: "synthetic-policy-v1",
	}
	return p, Input{Now: now, Search: search, Duration: 30 * time.Minute,
		CommitmentsComplete: true, CommitmentsCoverage: Interval{start.AddDate(0, 0, -14), start.AddDate(0, 0, 14)},
		Calendar: BusySnapshot{ObservedAt: now, Complete: true, Coverage: Interval{start.Add(-24 * time.Hour), search.End.Add(24 * time.Hour)}}}
}

func duration(d time.Duration) *time.Duration { return &d }

func instant(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}
