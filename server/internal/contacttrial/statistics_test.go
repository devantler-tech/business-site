package contacttrial

import "testing"

func TestNearestRank(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		values                 []int64
		numerator, denominator int
		want                   int64
	}{
		{"one", []int64{9}, 95, 100, 9},
		{"no interpolation", []int64{1, 2}, 50, 100, 1},
		{"upper tail", []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 99}, 95, 100, 99},
		{"negative differences", []int64{-99, -2, -1, 0}, 25, 100, -99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearestRank(tc.values, tc.numerator, tc.denominator); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBootstrapRecomputesQuantileDifference(t *testing.T) {
	// The Go distribution is constant, while half of the candidate journeys
	// have a second timing. The point median difference is zero, but resampled
	// medians cross the midpoint. A constant point-estimate interval is wrong.
	zero := 0
	r := run{Side: "go", ExitCode: &zero}
	s := run{Side: "svelte", ExitCode: &zero}
	makeJourney := func(i int, elapsed int64) journey {
		j := journey{Ordinal: i, SignIn: []int64{elapsed, elapsed, elapsed}}
		for _, spec := range phases()[3:] {
			status, size, duration := spec.status, int64(100), elapsed
			j.Responses = append(j.Responses, response{Actor: spec.actor, Phase: spec.phase, Status: &status, Bytes: &size, Elapsed: &duration})
		}
		return j
	}
	for i := 1; i <= 20; i++ {
		r.Warmups = append(r.Warmups, makeJourney(i, 900000000))
		s.Warmups = append(s.Warmups, makeJourney(i, 900000000))
	}
	for i := 1; i <= 100; i++ {
		r.Journeys = append(r.Journeys, makeJourney(i, 1000))
		elapsed := int64(1000)
		if i > 50 {
			elapsed = 2000
		}
		s.Journeys = append(s.Journeys, makeJourney(i, elapsed))
	}
	result := summarize(trial{Number: 1, Order: []string{"go", "svelte"}, Runs: []run{r, s}})
	for _, phase := range result.Phases {
		if phase.Difference.P50 != "0" || phase.Difference.P95 != "1000" || phase.P50Interval.Low != "0" || phase.P50Interval.High != "1000" || phase.P95Interval.Low != "1000" || phase.P95Interval.High != "1000" {
			t.Fatal("bootstrap must recompute both side quantiles within each paired resample")
		}
	}
}
