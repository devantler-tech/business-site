package contacttrial

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type report struct {
	Kind            string           `json:"kind"`
	Decision        string           `json:"decision"`
	InputSHA256     string           `json:"input_sha256"`
	EstimatorSHA256 string           `json:"estimator_sha256"`
	Declared        evidenceIdentity `json:"declared_provenance_not_verified"`
	Estimator       estimator        `json:"estimator"`
	Missing         []string         `json:"missing_evidence"`
	Trials          []trialReport    `json:"trials"`
}

type evidenceIdentity struct {
	Plan         string            `json:"plan_revision"`
	Amendment    string            `json:"amendment_revision"`
	Authority    string            `json:"authority_revision"`
	Baseline     string            `json:"baseline_revision"`
	Candidate    string            `json:"candidate_revision"`
	Dependencies map[string]string `json:"dependency_hashes"`
	Runs         []runIdentity     `json:"runs"`
}

type runIdentity struct {
	Trial      int    `json:"trial"`
	Side       string `json:"side"`
	Generation string `json:"generation"`
	Artifact   string `json:"artifact_sha256"`
	ExitCode   int    `json:"exit_code"`
}

type estimator struct {
	Quantiles string `json:"quantiles"`
	Bootstrap string `json:"bootstrap"`
	Samples   int    `json:"resamples"`
	Seed      int64  `json:"seed"`
}

// Execute runs only the explicitly invoked offline analyzer. Diagnostics never
// include input values, paths, raw JSON, credentials or filesystem errors.
func Execute(args []string, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("contact-trial-report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	input := flags.String("input", "", "owner-only raw timing JSON")
	output := flags.String("output", "", "new private report directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(diagnostics, "contact-trial-report --input <private-json> --output <new-directory>")
			return 0
		}
		_, _ = fmt.Fprintln(diagnostics, "invalid arguments; use --help")
		return 2
	}
	if *input == "" || *output == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(diagnostics, "explicit --input and --output are required; use --help")
		return 2
	}
	if err := analyzeFile(*input, *output); err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 1
	}
	_, _ = fmt.Fprintln(diagnostics, "timing report written; overall decision remains inconclusive")
	return 0
}

func analyzeFile(input, output string) error {
	data, err := privateInput(input)
	if err != nil {
		return errors.New("cannot read private timing evidence; use an owner-only regular file in a resolved directory")
	}
	e, err := decode(data)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("cannot identify estimator artifact")
	}
	binary, err := os.Open(executable)
	if err != nil {
		return errors.New("cannot read estimator artifact")
	}
	digest := sha256.New()
	_, hashErr := io.Copy(digest, binary)
	closeErr := binary.Close()
	if hashErr != nil || closeErr != nil {
		return errors.New("cannot hash estimator artifact")
	}
	r := report{Kind: "timing-analysis-only", Decision: "inconclusive", InputSHA256: hash(data), EstimatorSHA256: hex.EncodeToString(digest.Sum(nil)),
		Declared:  evidenceIdentity{Plan: e.Plan, Amendment: e.Amendment, Authority: e.Authority, Baseline: e.Baseline, Candidate: e.Candidate, Dependencies: e.Dependencies},
		Estimator: estimator{Quantiles: "nearest-rank", Bootstrap: "95% percentile; whole paired journeys; shared indices across phases; per trial", Samples: bootstrapSamples, Seed: bootstrapSeed},
		Missing:   []string{"independently verified process and source receipts", "separate-process RSS and restart measurements", "clean install and build measurements", "browser assets and accessibility proof", "maintenance source comparison", "holdouts and independent review", "same-state rollback proof"}}
	for _, t := range e.Trials {
		for _, run := range t.Runs {
			r.Declared.Runs = append(r.Declared.Runs, runIdentity{t.Number, run.Side, run.Generation, run.Artifact, *run.ExitCode})
		}
		r.Trials = append(r.Trials, summarize(t))
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return errors.New("cannot encode timing report")
	}
	return writeReport(output, append(encoded, '\n'))
}

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func resolvedParent(path string) (string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil || parent != filepath.Dir(abs) {
		return "", "", errors.New("unresolved parent")
	}
	return parent, filepath.Base(abs), nil
}

func privateInput(path string) (data []byte, err error) {
	parent, name, err := resolvedParent(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer func() {
		if root.Close() != nil {
			err = errInvalidEvidence
		}
	}()
	before, err := root.Lstat(name)
	if err != nil || !privateRegular(before) {
		return nil, errInvalidEvidence
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if f.Close() != nil {
			err = errInvalidEvidence
		}
	}()
	opened, err := f.Stat()
	if err != nil || !privateRegular(opened) || !os.SameFile(before, opened) || opened.Size() > maxInput {
		return nil, errInvalidEvidence
	}
	data, err = io.ReadAll(io.LimitReader(f, maxInput+1))
	if err != nil || len(data) > maxInput {
		return nil, errInvalidEvidence
	}
	return data, nil
}

func privateRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o177 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func writeReport(path string, data []byte) (err error) {
	failure := errors.New("cannot create private report; choose a new directory below a resolved parent")
	parent, name, err := resolvedParent(path)
	if err != nil {
		return failure
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return failure
	}
	defer func() {
		if root.Close() != nil {
			err = failure
		}
	}()
	// Mkdir is the reservation: an existing directory, file or symlink is refused.
	if err = root.Mkdir(name, 0o700); err != nil {
		return failure
	}
	dir, err := root.OpenRoot(name)
	if err != nil {
		return failure
	}
	defer func() {
		if dir.Close() != nil {
			err = failure
		}
	}()
	info, err := dir.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return failure
	}
	f, err := dir.OpenFile("timing-report.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return failure
	}
	_, writeErr := io.Copy(f, bytes.NewReader(data))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return failure
	}
	return nil
}
