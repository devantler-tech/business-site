// Package contacttrial analyzes the frozen private-contact experiment's timing
// observations offline. It neither collects evidence nor approves adoption.
package contacttrial

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const maxInput = 16 << 20

var errInvalidEvidence = errors.New("invalid timing evidence; check the documented closed schema and complete trial coverage")

type evidence struct {
	Schema       string            `json:"schema"`
	Plan         string            `json:"plan_revision"`
	Amendment    string            `json:"amendment_revision"`
	Authority    string            `json:"authority_revision"`
	Baseline     string            `json:"baseline_revision"`
	Candidate    string            `json:"candidate_revision"`
	Dependencies map[string]string `json:"dependency_hashes"`
	Trials       []trial           `json:"trials"`
}

type trial struct {
	Number int      `json:"number"`
	Order  []string `json:"order"`
	Runs   []run    `json:"runs"`
}

type run struct {
	Side       string    `json:"side"`
	Generation string    `json:"generation"`
	Artifact   string    `json:"artifact_sha256"`
	ExitCode   *int      `json:"exit_code"`
	Warmups    []journey `json:"warmups"`
	Journeys   []journey `json:"journeys"`
}

type journey struct {
	Ordinal   int        `json:"ordinal"`
	SignIn    []int64    `json:"sign_in_ns"`
	Responses []response `json:"responses"`
}

type response struct {
	Actor   string `json:"actor"`
	Phase   string `json:"phase"`
	Status  *int   `json:"status"`
	Bytes   *int64 `json:"bytes"`
	Elapsed *int64 `json:"elapsed_ns"`
}

type phaseSpec struct {
	actor, phase string
	status       int
}

func phases() []phaseSpec {
	var result []phaseSpec
	for _, actor := range []string{"client-a", "client-b", "operator"} {
		result = append(result, phaseSpec{actor, "sign-in", 0})
	}
	for _, actor := range []string{"client-a", "client-b"} {
		for i, phase := range []string{"workspace", "contact", "save", "reload", "stale-edit", "recover", "logout", "signed-out"} {
			result = append(result, phaseSpec{actor, phase, []int{200, 200, 303, 200, 409, 200, 303, 401}[i]})
		}
	}
	for i, phase := range []string{"workspace", "operator", "contact-denied", "logout", "signed-out"} {
		result = append(result, phaseSpec{"operator", phase, []int{200, 200, 403, 303, 403}[i]})
	}
	return result
}

func decode(data []byte) (evidence, error) {
	var result evidence
	if len(data) == 0 || len(data) > maxInput {
		return result, errInvalidEvidence
	}
	// DisallowUnknownFields does not reject duplicate keys. Examine every nested
	// object before typed extraction so an overwritten finding cannot disappear.
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(decoder, 0); err != nil {
		return result, errInvalidEvidence
	}
	if _, err := decoder.Token(); err != io.EOF {
		return result, errInvalidEvidence
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, errInvalidEvidence
	}
	if err := result.validate(); err != nil {
		return result, err
	}
	return result, nil
}

func uniqueValue(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return errInvalidEvidence
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			key, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			name, ok := key.(string)
			if !ok || keys[name] || strings.ToLower(name) != name {
				return errInvalidEvidence
			}
			keys[name] = true
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errInvalidEvidence
	}
	end, err := decoder.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errInvalidEvidence
	}
	return nil
}

func (e evidence) validate() error {
	if e.Schema != "private-contact-timings/v1" || e.Plan != "868d7acdeb4f822d0f15758e15b1539c245f5b53" || e.Amendment != "a4ee85c7b8e7a776a43dcc37071d39bbf0b956df" || e.Authority != "a1be78797e7886e3eb57605792027af6d4811add" || !hexID(e.Baseline, 40) || !hexID(e.Candidate, 40) || len(e.Trials) != 3 {
		return errInvalidEvidence
	}
	wantDependencies := map[string]string{
		"package-lock.json":     "fec6fd85de707023da30c1ccb6984921ffde4424153014db03042123a2408be7",
		"web/package-lock.json": "af00c10f41f41cbc3605f93b7a68d9d133395d1ebfbecae38b1632eee4720d45",
		"server/go.mod":         "fbbd640cf43af89f1974724f3c59bff49889b8e9cab7b70cc80d14eca840b54e",
		"server/go.sum":         "e9d42cc756bf9c1790418b0cb257d5621599d5ebceb30be9238180ef12d3aa07",
	}
	if len(e.Dependencies) != len(wantDependencies) {
		return errInvalidEvidence
	}
	for key, value := range wantDependencies {
		if e.Dependencies[key] != value {
			return errInvalidEvidence
		}
	}
	generations := map[string]bool{}
	for i, t := range e.Trials {
		order := []string{"go", "svelte"}
		if i == 1 {
			order = []string{"svelte", "go"}
		}
		if t.Number != i+1 || len(t.Order) != 2 || len(t.Runs) != 2 {
			return errInvalidEvidence
		}
		for j, r := range t.Runs {
			if t.Order[j] != order[j] || r.Side != order[j] || !hexID(r.Generation, 32) || generations[r.Generation] || !hexID(r.Artifact, 64) || r.ExitCode == nil || *r.ExitCode < 0 || *r.ExitCode > 255 || len(r.Warmups) != 20 || len(r.Journeys) != 100 {
				return errInvalidEvidence
			}
			generations[r.Generation] = true
			for _, set := range [][]journey{r.Warmups, r.Journeys} {
				for k, observation := range set {
					if err := observation.validate(k + 1); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func hexID(value string, size int) bool {
	if len(value) != size {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	for _, b := range decoded {
		if b != 0 {
			return true
		}
	}
	return false
}

func (j journey) validate(ordinal int) error {
	if j.Ordinal != ordinal || len(j.SignIn) != 3 || len(j.Responses) != 21 {
		return errInvalidEvidence
	}
	for _, elapsed := range j.SignIn {
		if elapsed < 0 {
			return errInvalidEvidence
		}
	}
	for i, spec := range phases()[3:] {
		r := j.Responses[i]
		if r.Actor != spec.actor || r.Phase != spec.phase || r.Status == nil || *r.Status < 0 || *r.Status > 599 || r.Bytes == nil || *r.Bytes < 0 || r.Elapsed == nil || *r.Elapsed < 0 {
			return errInvalidEvidence
		}
	}
	return nil
}
