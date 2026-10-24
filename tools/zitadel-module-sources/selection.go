package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/mod/module"
)

type identity struct {
	Path, Version, Sum, GoModSum string
	Main                         bool
	Replace                      *identity
}

type entry struct {
	Original   identity        `json:"original"`
	Effective  identity        `json:"effective"`
	Components json.RawMessage `json:"components"`
	Origins    json.RawMessage `json:"origins,omitempty"`
}

type catalogue struct {
	SchemaVersion                   int
	UpstreamCommit, MetadataProfile string
	GoModSHA256, GoSumSHA256        string
	Modules                         []entry
}

// selectModules joins the complete compiler observation to frozen module inputs.
func selectModules(raw, mod, sums, packages []byte, build, expected string) ([]entry, error) {
	if !filepath.IsAbs(build) || filepath.Clean(build) != build || hash(raw) != expected {
		return nil, fmt.Errorf("module catalogue or build root differs")
	}
	var c catalogue
	if err := decodeStrict(raw, &c); err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	if c.SchemaVersion != 1 || c.UpstreamCommit != "8afa5dd37a8e723efb9eace8d09fd5c1cccef694" || c.MetadataProfile != "machine-id-no-jsonpath-v1" || len(c.Modules) == 0 || len(c.Modules) > 512 || hash(mod) != c.GoModSHA256 || hash(sums) != c.GoSumSHA256 {
		return nil, fmt.Errorf("frozen module inputs differ")
	}
	wantSums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || wantSums[fields[0]+" "+fields[1]] != "" {
			return nil, fmt.Errorf("incomplete or duplicate frozen sum")
		}
		wantSums[fields[0]+" "+fields[1]] = fields[2]
	}
	wanted := map[string]entry{}
	previous := ""
	for _, selected := range c.Modules {
		o, e := selected.Original, selected.Effective
		if o.Path <= previous || o.Main || e.Main || e.Replace != nil || module.Check(o.Path, o.Version) != nil || module.Check(e.Path, e.Version) != nil || module.CanonicalVersion(e.Version) != e.Version || !contentSum(e.Sum) || !contentSum(e.GoModSum) {
			return nil, fmt.Errorf("invalid or unordered module identity")
		}
		resolved := o
		if o.Replace != nil {
			resolved = *o.Replace
		}
		// These are small recursive identity records, not runtime package objects.
		if !reflect.DeepEqual(resolved, e) || wantSums[e.Path+" "+e.Version] != e.Sum || wantSums[e.Path+" "+e.Version+"/go.mod"] != e.GoModSum {
			return nil, fmt.Errorf("replacement or complete checksum differs")
		}
		wanted[o.Path], previous = selected, o.Path
	}
	seen, seenModules := map[string]bool{}, map[string]bool{}
	var rootDeps []string
	sawRoot := false
	stream := json.NewDecoder(bytes.NewReader(packages))
	for {
		var rawPackage json.RawMessage
		if err := stream.Decode(&rawPackage); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("incomplete compiler observation: %w", err)
		}
		if err := uniqueJSON(rawPackage); err != nil {
			return nil, err
		}
		var p struct {
			ImportPath, Dir, Name string
			Standard, Incomplete  bool
			Error                 json.RawMessage
			DepsErrors            []json.RawMessage
			Deps                  []string
			Module                json.RawMessage
		}
		if err := json.Unmarshal(rawPackage, &p); err != nil || p.ImportPath == "" || seen[p.ImportPath] || p.Incomplete || (len(p.Error) != 0 && string(p.Error) != "null") || len(p.DepsErrors) != 0 {
			return nil, fmt.Errorf("invalid, incomplete or duplicate package")
		}
		seen[p.ImportPath] = true
		if len(p.Module) == 0 || string(p.Module) == "null" {
			if !p.Standard {
				return nil, fmt.Errorf("nonstandard package lacks module")
			}
			continue
		}
		var observed identity
		var location struct {
			Dir, GoMod string
			Error      json.RawMessage
			Replace    *struct {
				Dir, GoMod string
				Error      json.RawMessage
			}
		}
		if json.Unmarshal(p.Module, &observed) != nil || json.Unmarshal(p.Module, &location) != nil || p.Standard || (len(location.Error) != 0 && string(location.Error) != "null") {
			return nil, fmt.Errorf("invalid module observation")
		}
		if observed.Main {
			if !reflect.DeepEqual(observed, identity{Path: "github.com/zitadel/zitadel", Main: true}) || location.Dir != filepath.Join(build, "prepared/source") || location.GoMod != filepath.Join(build, "prepared/source/go.mod") {
				return nil, fmt.Errorf("wrong root module observation")
			}
			if p.ImportPath == "github.com/zitadel/zitadel" {
				if p.Name != "main" {
					return nil, fmt.Errorf("root package is not the service command")
				}
				sawRoot = true
				rootDeps = p.Deps
			}
		} else {
			selected, ok := wanted[observed.Path]
			if !ok || !reflect.DeepEqual(observed, selected.Original) {
				return nil, fmt.Errorf("compiler module differs from catalogue")
			}
			ePath, err := module.EscapePath(selected.Effective.Path)
			if err != nil {
				return nil, err
			}
			eVersion, err := module.EscapeVersion(selected.Effective.Version)
			if err != nil {
				return nil, err
			}
			if location.Dir != filepath.Join(build, "go-modules", ePath+"@"+eVersion) || location.GoMod != filepath.Join(build, "go-modules/cache/download", ePath, "@v", eVersion+".mod") {
				return nil, fmt.Errorf("module outside build-owned cache")
			}
			if location.Replace != nil && (location.Replace.Dir != location.Dir || location.Replace.GoMod != location.GoMod || (len(location.Replace.Error) != 0 && string(location.Replace.Error) != "null")) {
				return nil, fmt.Errorf("replacement cache differs")
			}
			seenModules[observed.Path] = true
		}
		if p.ImportPath != observed.Path && !strings.HasPrefix(p.ImportPath, observed.Path+"/") {
			return nil, fmt.Errorf("package outside selected module")
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(p.ImportPath, observed.Path), "/")
		if (suffix != "" && (!filepath.IsLocal(suffix) || filepath.ToSlash(filepath.Clean(suffix)) != suffix || strings.Contains(suffix, "\\"))) || p.Dir != filepath.Join(location.Dir, suffix) {
			return nil, fmt.Errorf("package directory differs from import identity")
		}
	}
	if !sawRoot || len(seenModules) != len(wanted) || len(rootDeps)+1 != len(seen) {
		return nil, fmt.Errorf("compiler observation lacks selected modules")
	}
	dependencies := map[string]bool{}
	for _, dependency := range rootDeps {
		if dependency == "" || dependency == "github.com/zitadel/zitadel" || dependencies[dependency] || !seen[dependency] {
			return nil, fmt.Errorf("root dependency closure differs from complete compiler observation")
		}
		dependencies[dependency] = true
	}
	return c.Modules, nil
}

// hash identifies literal bytes; module content sums use dirhash instead.
func hash(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

// contentSum requires a complete SHA-256 Go content checksum.
func contentSum(sum string) bool {
	if !strings.HasPrefix(sum, "h1:") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sum, "h1:"))
	return err == nil && len(decoded) == sha256.Size
}

// decodeStrict refuses duplicate fields, unknown schema fields and trailing JSON.
func decodeStrict(raw []byte, out any) error {
	if err := uniqueJSON(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

// uniqueJSON checks every nested object before Go's decoder can overwrite a key.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("metadata nesting exceeds limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		if delim != '{' && delim != '[' {
			return fmt.Errorf("unexpected JSON closing token")
		}
		for d.More() {
			if delim == '{' {
				token, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return fmt.Errorf("duplicate or malformed JSON key")
				}
				seen[key] = true
			}
			if err := visit(depth + 1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing or incomplete JSON")
	}
	return nil
}
