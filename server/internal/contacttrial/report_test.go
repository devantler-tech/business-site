package contacttrial_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/contacttrial"
)

// This exercises the actual offline command, not a substitute estimator.
func TestTimingReportCommand(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "contact-report")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/contact-trial-report")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("offline timing command is unavailable: %v\n%s", err, out)
	}
	input := syntheticEvidence()
	inputPath := filepath.Join(dir, "synthetic.json")
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(inputPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "report")
	cmd := exec.Command(binary, "--input", inputPath, "--output", output)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("complete synthetic evidence must be analyzed: %v\n%s", runErr, out)
	}
	reportBytes, err := os.ReadFile(filepath.Join(output, "timing-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Kind, Decision  string
		InputSHA256     string   `json:"input_sha256"`
		EstimatorSHA256 string   `json:"estimator_sha256"`
		Missing         []string `json:"missing_evidence"`
		Trials          []struct {
			Warmups  int `json:"excluded_warmups"`
			Measured int `json:"measured_journeys"`
			Failures int `json:"failed_observations"`
			Phases   []struct {
				Actor, Phase           string
				Go, Svelte, Difference struct {
					P50 string `json:"p50_ns"`
					P95 string `json:"p95_ns"`
				}
				P50Interval struct{ Low, High string } `json:"p50_interval_ns"`
				P95Interval struct{ Low, High string } `json:"p95_interval_ns"`
			}
		}
	}
	if err = json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "timing-analysis-only" || report.Decision != "inconclusive" || len(report.Missing) < 6 {
		t.Fatal("timing evidence must not become an adoption or complete-trial verdict")
	}
	wantInput := sha256.Sum256(data)
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	wantEstimator := sha256.Sum256(executable)
	if report.InputSHA256 != hex.EncodeToString(wantInput[:]) || report.EstimatorSHA256 != hex.EncodeToString(wantEstimator[:]) {
		t.Fatal("report must bind the raw input and actual running estimator")
	}
	if len(report.Trials) != 3 {
		t.Fatal("trials must not be pooled or omitted")
	}
	for _, trial := range report.Trials {
		if trial.Warmups != 40 || trial.Measured != 200 || trial.Failures != 0 || len(trial.Phases) != 24 {
			t.Fatal("complete actor phases and all warmups/journeys must be counted")
		}
		for _, phase := range trial.Phases {
			// Each candidate observation is its matching Go observation +500 ns.
			// Independent resampling, pooled sign-ins, interpolation, or including
			// the deliberately slow warmups breaks these hand-derived values.
			if phase.Difference.P50 != "500" || phase.Difference.P95 != "500" || phase.P50Interval.Low != "500" || phase.P50Interval.High != "500" || phase.P95Interval.Low != "500" || phase.P95Interval.High != "500" {
				t.Fatalf("lost whole-journey pairing in %s/%s", phase.Actor, phase.Phase)
			}
			if phase.Actor == "client-a" && phase.Phase == "contact" && (phase.Go.P50 != "51000" || phase.Go.P95 != "96000" || phase.Svelte.P50 != "51500" || phase.Svelte.P95 != "96500") {
				t.Fatal("nearest-rank contact quantiles are incorrect")
			}
		}
	}
	for _, path := range []string{output, filepath.Join(output, "timing-report.json")} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatal("private output grants group/other access")
		}
	}
	if out, runErr := cmd.CombinedOutput(); runErr == nil || strings.Contains(string(out), inputPath) {
		t.Fatal("an existing output must be refused without exposing input paths")
	}
	second := filepath.Join(dir, "second-report")
	if out, runErr := exec.Command(binary, "--input", inputPath, "--output", second).CombinedOutput(); runErr != nil {
		t.Fatalf("deterministic repeat failed: %v %s", runErr, out)
	}
	repeated, err := os.ReadFile(filepath.Join(second, "timing-report.json"))
	if err != nil || !bytes.Equal(reportBytes, repeated) {
		t.Fatal("identical bytes and estimator must yield identical reports")
	}
	retained, err := os.ReadFile(inputPath)
	if err != nil || !bytes.Equal(data, retained) {
		t.Fatal("raw evidence must remain unchanged")
	}
}

func TestInvalidEvidenceCannotProduceReport(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		raw    func([]byte) []byte
	}{
		{name: "case alias", raw: func(b []byte) []byte { return bytes.Replace(b, []byte(`"schema":`), []byte(`"Schema":`), 1) }},
		{name: "duplicate key", raw: func(b []byte) []byte { return append([]byte(`{"schema":"fictional-private-marker",`), b[1:]...) }},
		{name: "nested duplicate", raw: func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"status":200`), []byte(`"status":500,"status":200`), 1)
		}},
		{name: "unknown field", mutate: func(e map[string]any) { e["private-extra"] = "fictional-private-marker" }},
		{name: "trailing document", raw: func(b []byte) []byte { return append(b, []byte(` {}`)...) }},
		{name: "truncated final trial", raw: func(b []byte) []byte { return b[:len(b)-8] }},
		{name: "oversized", raw: func([]byte) []byte { return bytes.Repeat([]byte(" "), (16<<20)+1) }},
		{name: "wrong authority", mutate: func(e map[string]any) { e["authority_revision"] = strings.Repeat("d", 40) }},
		{name: "missing dependency", mutate: func(e map[string]any) { delete(e["dependency_hashes"].(map[string]string), "server/go.sum") }},
		{name: "missing trial", mutate: func(e map[string]any) { e["trials"] = e["trials"].([]any)[:2] }},
		{name: "wrong order", mutate: func(e map[string]any) { firstTrial(e)["order"] = []string{"svelte", "go"} }},
		{name: "missing run exit", mutate: func(e map[string]any) { delete(firstRun(e), "exit_code") }},
		{name: "reused generation", mutate: func(e map[string]any) {
			firstTrial(e)["runs"].([]any)[1].(map[string]any)["generation"] = firstRun(e)["generation"]
		}},
		{name: "missing ordinal", mutate: func(e map[string]any) { firstJourney(e)["ordinal"] = 2 }},
		{name: "missing warmup", mutate: func(e map[string]any) { firstRun(e)["warmups"] = firstRun(e)["warmups"].([]any)[:19] }},
		{name: "missing final journey", mutate: func(e map[string]any) { firstRun(e)["journeys"] = firstRun(e)["journeys"].([]any)[:99] }},
		{name: "missing phase", mutate: func(e map[string]any) { firstJourney(e)["responses"] = firstJourney(e)["responses"].([]any)[:20] }},
		{name: "negative timing", mutate: func(e map[string]any) { firstResponse(e)["elapsed_ns"] = int64(-1) }},
		{name: "missing timing", mutate: func(e map[string]any) { delete(firstResponse(e), "elapsed_ns") }},
		{name: "missing status", mutate: func(e map[string]any) { delete(firstResponse(e), "status") }},
		{name: "wrong actor", mutate: func(e map[string]any) { firstResponse(e)["actor"] = "operator" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := syntheticEvidence()
			if tc.mutate != nil {
				tc.mutate(e)
			}
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if tc.raw != nil {
				data = tc.raw(data)
			}
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			input, output := filepath.Join(dir, "evidence.json"), filepath.Join(dir, "report")
			if err = os.WriteFile(input, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			if contacttrial.Execute([]string{"--input", input, "--output", output}, &diagnostic) == 0 {
				t.Fatal("invalid or incomplete evidence produced success")
			}
			if _, err = os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid evidence produced a report directory")
			}
			if strings.Contains(diagnostic.String(), input) || strings.Contains(diagnostic.String(), "fictional-private-marker") {
				t.Fatal("diagnostic exposed raw input")
			}
		})
	}
}

func TestObservedFailuresRemainVisible(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mutate       func(map[string]any)
		failed, runs int
	}{
		{"response failure", func(e map[string]any) { firstResponse(e)["status"] = 500 }, 1, 0},
		{"unobserved sign-in", func(e map[string]any) { firstJourney(e)["sign_in_ns"] = []int64{0, 1, 1} }, 1, 0},
		{"failed warmup", func(e map[string]any) {
			firstRun(e)["warmups"].([]any)[0].(map[string]any)["sign_in_ns"] = []int64{0, 1, 1}
		}, 1, 0},
		{"failed process", func(e map[string]any) { firstRun(e)["exit_code"] = 1 }, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := syntheticEvidence()
			tc.mutate(e)
			// All trials fail one run here so this test checks retention without
			// repeatedly exercising unrelated bootstrap work in the healthy trials.
			for _, raw := range e["trials"].([]any)[1:] {
				raw.(map[string]any)["runs"].([]any)[0].(map[string]any)["exit_code"] = 1
			}
			r := executeEvidence(t, e)
			var result struct {
				Decision string
				Trials   []struct {
					State    string
					Failures int `json:"failed_observations"`
					Runs     int `json:"failed_runs"`
					Phases   []any
				}
			}
			if err := json.Unmarshal(r, &result); err != nil {
				t.Fatal(err)
			}
			first := result.Trials[0]
			if result.Decision != "inconclusive" || first.State != "observed-failure" || first.Failures != tc.failed || first.Runs != tc.runs || len(first.Phases) != 0 {
				t.Fatal("failure disappeared into survivor quantiles")
			}
		})
	}
}

func TestPrivateFileBoundaries(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "evidence.json")
	data, err := json.Marshal(syntheticEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.json")
	if err = os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if contacttrial.Execute([]string{"--input", link, "--output", filepath.Join(dir, "symlink-report")}, &diagnostic) == 0 {
		t.Fatal("symlink input admitted")
	}
	if err = os.Chmod(input, 0o640); err != nil {
		t.Fatal(err)
	}
	if contacttrial.Execute([]string{"--input", input, "--output", filepath.Join(dir, "broad-report")}, &diagnostic) == 0 {
		t.Fatal("group-readable input admitted")
	}
	if err = os.Chmod(input, 0o600); err != nil {
		t.Fatal(err)
	}
	// A failing trial avoids bootstrap cost; valid evidence still reaches the
	// actual filesystem writer, which must reject an existing/symlink output.
	e := syntheticEvidence()
	for _, raw := range e["trials"].([]any) {
		raw.(map[string]any)["runs"].([]any)[0].(map[string]any)["exit_code"] = 1
	}
	data, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "existing")
	if err = os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	outputLink := filepath.Join(dir, "output-link")
	if err = os.Symlink(target, outputLink); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(dir, "parent-link")
	if err = os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{target, outputLink, filepath.Join(parentLink, "new-report")} {
		if contacttrial.Execute([]string{"--input", input, "--output", output}, &diagnostic) == 0 {
			t.Fatal("unsafe/existing output admitted")
		}
	}
	if _, err = os.Stat(filepath.Join(target, "timing-report.json")); !os.IsNotExist(err) {
		t.Fatal("existing output modified")
	}
}

func TestExplicitInvocationOnly(t *testing.T) {
	for _, args := range [][]string{nil, {"--input", "fictional-private-marker"}, {"--output", "fictional-private-marker"}, {"--unknown=fictional-private-marker"}, {"unexpected-fictional-private-marker"}} {
		var diagnostic bytes.Buffer
		if contacttrial.Execute(args, &diagnostic) != 2 || strings.Contains(diagnostic.String(), "fictional-private-marker") {
			t.Fatal("missing/invalid opt-in must fail without echoing arguments")
		}
	}
	var diagnostic bytes.Buffer
	if contacttrial.Execute([]string{"--help"}, &diagnostic) != 0 || !strings.Contains(diagnostic.String(), "--input") {
		t.Fatal("help must describe explicit offline invocation")
	}
}

func executeEvidence(t *testing.T, e map[string]any) []byte {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(dir, "input.json"), filepath.Join(dir, "report")
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	if contacttrial.Execute([]string{"--input", input, "--output", output}, &diagnostic) != 0 {
		t.Fatalf("analysis failed: %s", diagnostic.String())
	}
	report, err := os.ReadFile(filepath.Join(output, "timing-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func firstTrial(e map[string]any) map[string]any { return e["trials"].([]any)[0].(map[string]any) }
func firstRun(e map[string]any) map[string]any {
	return firstTrial(e)["runs"].([]any)[0].(map[string]any)
}
func firstJourney(e map[string]any) map[string]any {
	return firstRun(e)["journeys"].([]any)[0].(map[string]any)
}
func firstResponse(e map[string]any) map[string]any {
	return firstJourney(e)["responses"].([]any)[0].(map[string]any)
}

func syntheticEvidence() map[string]any {
	trials := make([]any, 3)
	orders := [][]string{{"go", "svelte"}, {"svelte", "go"}, {"go", "svelte"}}
	for trial, order := range orders {
		runs := make([]any, 2)
		for side, name := range order {
			warmups, journeys := make([]any, 20), make([]any, 100)
			for i := range warmups {
				warmups[i] = syntheticJourney(i+1, 800000000)
			}
			for i := range journeys {
				delta := int64(0)
				if name == "svelte" {
					delta = 500
				}
				journeys[i] = syntheticJourney(i+1, int64(i+1)*1000+delta)
			}
			runs[side] = map[string]any{"side": name, "generation": strings.Repeat(string(rune('a'+trial*2+side)), 32), "artifact_sha256": strings.Repeat("a", 64), "exit_code": 0, "warmups": warmups, "journeys": journeys}
		}
		trials[trial] = map[string]any{"number": trial + 1, "order": order, "runs": runs}
	}
	return map[string]any{
		"schema":             "private-contact-timings/v1",
		"plan_revision":      "868d7acdeb4f822d0f15758e15b1539c245f5b53",
		"amendment_revision": "a4ee85c7b8e7a776a43dcc37071d39bbf0b956df",
		"authority_revision": "a1be78797e7886e3eb57605792027af6d4811add",
		"baseline_revision":  strings.Repeat("b", 40), "candidate_revision": strings.Repeat("c", 40),
		"dependency_hashes": map[string]string{
			"package-lock.json":     "fec6fd85de707023da30c1ccb6984921ffde4424153014db03042123a2408be7",
			"web/package-lock.json": "af00c10f41f41cbc3605f93b7a68d9d133395d1ebfbecae38b1632eee4720d45",
			"server/go.mod":         "fbbd640cf43af89f1974724f3c59bff49889b8e9cab7b70cc80d14eca840b54e",
			"server/go.sum":         "e9d42cc756bf9c1790418b0cb257d5621599d5ebceb30be9238180ef12d3aa07",
		}, "trials": trials,
	}
}

func syntheticJourney(ordinal int, elapsed int64) map[string]any {
	samples := []any{}
	for _, actor := range []string{"client-a", "client-b", "operator"} {
		phases := []string{"workspace", "contact", "save", "reload", "stale-edit", "recover", "logout", "signed-out"}
		statuses := []int{200, 200, 303, 200, 409, 200, 303, 401}
		if actor == "operator" {
			phases = []string{"workspace", "operator", "contact-denied", "logout", "signed-out"}
			statuses = []int{200, 200, 403, 303, 403}
		}
		for i, phase := range phases {
			samples = append(samples, map[string]any{"actor": actor, "phase": phase, "status": statuses[i], "bytes": 100, "elapsed_ns": elapsed + int64(i)*1000})
		}
	}
	return map[string]any{"ordinal": ordinal, "sign_in_ns": []int64{elapsed + 10000000, elapsed + 20000000, elapsed + 30000000}, "responses": samples}
}
