package contacttrial

import (
	"math/rand"
	"slices"
	"strconv"
)

const bootstrapSamples = 10000
const bootstrapSeed = 199200

type quantiles struct {
	P50 string `json:"p50_ns"`
	P95 string `json:"p95_ns"`
}

type interval struct {
	Low  string `json:"low"`
	High string `json:"high"`
}

type phaseReport struct {
	Actor       string    `json:"actor"`
	Phase       string    `json:"phase"`
	Go          quantiles `json:"go"`
	Svelte      quantiles `json:"svelte"`
	Difference  quantiles `json:"difference"`
	P50Interval interval  `json:"p50_interval_ns"`
	P95Interval interval  `json:"p95_interval_ns"`
}

type trialReport struct {
	Number     int           `json:"number"`
	Order      []string      `json:"order"`
	Warmups    int           `json:"excluded_warmups"`
	Measured   int           `json:"measured_journeys"`
	Failures   int           `json:"failed_observations"`
	FailedRuns int           `json:"failed_runs"`
	State      string        `json:"state"`
	Phases     []phaseReport `json:"phases"`
}

// nearestRank uses one-based ceil(n*numerator/denominator), never interpolation.
func nearestRank(sorted []int64, numerator, denominator int) int64 {
	return sorted[(len(sorted)*numerator+denominator-1)/denominator-1]
}

func summarize(t trial) trialReport {
	result := trialReport{Number: t.Number, Order: t.Order, Warmups: 40, Measured: 200, State: "timings-analyzed", Phases: []phaseReport{}}
	var goRun, svelteRun run
	for _, r := range t.Runs {
		if r.Side == "go" {
			goRun = r
		} else {
			svelteRun = r
		}
		if *r.ExitCode != 0 {
			result.FailedRuns++
		}
		for _, set := range [][]journey{r.Warmups, r.Journeys} {
			for _, j := range set {
				for _, elapsed := range j.SignIn {
					if elapsed == 0 {
						result.Failures++
					}
				}
				for i, spec := range phases()[3:] {
					if *j.Responses[i].Status != spec.status || *j.Responses[i].Elapsed == 0 {
						result.Failures++
					}
				}
			}
		}
	}
	// Never drop a failed attempt and report quantiles over only survivors.
	if result.Failures != 0 || result.FailedRuns != 0 {
		result.State = "observed-failure"
		return result
	}
	goValues, svelteValues := observations(goRun), observations(svelteRun)
	p50Differences, p95Differences := make([][]int64, 24), make([][]int64, 24)
	for i, spec := range phases() {
		g, s := slices.Clone(goValues[i]), slices.Clone(svelteValues[i])
		slices.Sort(g)
		slices.Sort(s)
		result.Phases = append(result.Phases, phaseReport{Actor: spec.actor, Phase: spec.phase,
			Go: quantiles{decimal(g[49]), decimal(g[94])}, Svelte: quantiles{decimal(s[49]), decimal(s[94])},
			Difference: quantiles{decimal(s[49] - g[49]), decimal(s[94] - g[94])}})
		p50Differences[i] = make([]int64, bootstrapSamples)
		p95Differences[i] = make([]int64, bootstrapSamples)
	}
	// One vector of paired journey ordinals per resample, shared by every phase
	// (including each separate actor's sign-in). Never resample the two sides.
	random := rand.New(rand.NewSource(bootstrapSeed))
	var indices [100]int
	var g, s [100]int64
	for sample := range bootstrapSamples {
		for i := range indices {
			indices[i] = random.Intn(100)
		}
		for phase := range goValues {
			for i, ordinal := range indices {
				g[i], s[i] = goValues[phase][ordinal], svelteValues[phase][ordinal]
			}
			slices.Sort(g[:])
			slices.Sort(s[:])
			p50Differences[phase][sample] = s[49] - g[49]
			p95Differences[phase][sample] = s[94] - g[94]
		}
	}
	for phase := range result.Phases {
		slices.Sort(p50Differences[phase])
		slices.Sort(p95Differences[phase])
		result.Phases[phase].P50Interval = interval{decimal(nearestRank(p50Differences[phase], 25, 1000)), decimal(nearestRank(p50Differences[phase], 975, 1000))}
		result.Phases[phase].P95Interval = interval{decimal(nearestRank(p95Differences[phase], 25, 1000)), decimal(nearestRank(p95Differences[phase], 975, 1000))}
	}
	return result
}

func observations(r run) [][]int64 {
	values := make([][]int64, 24)
	for i := range values {
		values[i] = make([]int64, 100)
	}
	for i, j := range r.Journeys {
		for phase, elapsed := range j.SignIn {
			values[phase][i] = elapsed
		}
		for phase, response := range j.Responses {
			values[phase+3][i] = *response.Elapsed
		}
	}
	return values
}

func decimal(value int64) string { return strconv.FormatInt(value, 10) }
