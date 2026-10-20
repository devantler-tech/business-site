// Package trialinput checks immutable source identities for the private contact experiment.
package trialinput

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Inputs names only full immutable commit identities, never refs or expressions.
type Inputs struct {
	Plan, Baseline, Candidate string
}

// Source binds an admitted commit to its tree and complete presentation delta.
type Source struct {
	Commit  string   `json:"commit"`
	Tree    string   `json:"tree"`
	Changed []string `json:"changed"`
}

// Receipt is source admission only, not a measurement or authorization verdict.
type Receipt struct {
	Scope           string            `json:"scope"`
	Plan            string            `json:"plan"`
	Amendment       string            `json:"amendment"`
	SharedAuthority string            `json:"sharedAuthority"`
	Baseline        Source            `json:"baseline"`
	Candidate       Source            `json:"candidate"`
	Dependencies    map[string]string `json:"dependencies"`
}

type policy struct {
	plan, amendment, shared string
	pins                    map[string]string
	documents               []document
}

type document struct{ commit, path, digest string }

var errRefused = errors.New("trial source input refused")
var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Check reads local Git objects only. It performs no network, build or trial work.
// The frozen policy changes only through a reviewed experiment amendment.
func Check(ctx context.Context, repository string, input Inputs) (Receipt, error) {
	return check(ctx, repository, input, policy{
		plan:      "868d7acdeb4f822d0f15758e15b1539c245f5b53",
		amendment: "a4ee85c7b8e7a776a43dcc37071d39bbf0b956df",
		shared:    "a1be78797e7886e3eb57605792027af6d4811add",
		pins: map[string]string{
			"package-lock.json":     "fec6fd85de707023da30c1ccb6984921ffde4424153014db03042123a2408be7",
			"web/package-lock.json": "af00c10f41f41cbc3605f93b7a68d9d133395d1ebfbecae38b1632eee4720d45",
			"server/go.mod":         "fbbd640cf43af89f1974724f3c59bff49889b8e9cab7b70cc80d14eca840b54e",
			"server/go.sum":         "e9d42cc756bf9c1790418b0cb257d5621599d5ebceb30be9238180ef12d3aa07",
		},
		documents: []document{
			{"868d7acdeb4f822d0f15758e15b1539c245f5b53", "docs/adr/0014-private-contact-renderer-experiment.md", "2b581d85990e60ee482c73e2d4c1769785814a3eec989b9c2a60c2573aa772d6"},
			{"a4ee85c7b8e7a776a43dcc37071d39bbf0b956df", "docs/adr/0015-private-contact-comparator-amendment.md", "698c4e571b6551e44317488cbbf117a8234e4686028ad13813dca18eeb59a219"},
		},
	})
}

func check(ctx context.Context, repository string, input Inputs, p policy) (Receipt, error) {
	refuse := func() (Receipt, error) { return Receipt{}, errRefused }
	if input.Plan != p.plan {
		return refuse()
	}
	prefix, err := git(ctx, repository, "rev-parse", "--show-prefix")
	if err != nil || string(prefix) != "\n" {
		return refuse()
	}
	for _, id := range []string{input.Plan, p.amendment, p.shared, input.Baseline, input.Candidate} {
		if !commitID.MatchString(id) {
			return refuse()
		}
		kind, err := git(ctx, repository, "cat-file", "-t", id)
		if err != nil || string(kind) != "commit\n" {
			return refuse()
		}
	}
	keys, err := git(ctx, repository, "config", "--includes", "--null", "--name-only", "--list")
	if err != nil {
		return refuse()
	}
	for _, key := range strings.Split(string(keys), "\x00") {
		if strings.HasPrefix(strings.ToLower(key), "filter.") {
			// Status must not run checkout-configured clean/process filters.
			return refuse()
		}
	}
	status, err := git(ctx, repository, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil || len(status) != 0 {
		return refuse()
	}
	indexed, err := git(ctx, repository, "ls-files", "-v", "-z")
	if err != nil || len(indexed) == 0 || indexed[len(indexed)-1] != 0 {
		return refuse()
	}
	for _, entry := range strings.Split(string(indexed[:len(indexed)-1]), "\x00") {
		if !strings.HasPrefix(entry, "H ") {
			// skip-worktree and assume-unchanged can conceal modified inputs.
			return refuse()
		}
	}
	for _, doc := range p.documents {
		if !hasDigest(ctx, repository, doc.commit, doc.path, doc.digest) {
			return refuse()
		}
	}
	baseline, err := source(ctx, repository, input.Baseline, p, false)
	if err != nil {
		return refuse()
	}
	candidate, err := source(ctx, repository, input.Candidate, p, true)
	if err != nil {
		return refuse()
	}
	return Receipt{Scope: "source-inputs-only", Plan: p.plan, Amendment: p.amendment,
		SharedAuthority: p.shared, Baseline: baseline, Candidate: candidate, Dependencies: p.pins}, nil
}

func source(ctx context.Context, repository, id string, p policy, candidate bool) (Source, error) {
	if _, err := git(ctx, repository, "merge-base", "--is-ancestor", p.shared, id); err != nil {
		return Source{}, errRefused
	}
	for path, want := range p.pins {
		if !hasDigest(ctx, repository, id, path, want) {
			return Source{}, errRefused
		}
	}
	changed, err := git(ctx, repository, "diff", "--no-ext-diff", "--no-textconv", "--no-relative", "--no-renames", "--name-only", "-z", p.shared, id, "--")
	if err != nil {
		return Source{}, errRefused
	}
	paths := []string{}
	if len(changed) != 0 {
		if changed[len(changed)-1] != 0 {
			return Source{}, errRefused
		}
		paths = strings.Split(string(changed[:len(changed)-1]), "\x00")
	}
	for _, path := range paths {
		allowed := path == "server/internal/identity/web/portal.html" || path == "server/internal/identity/web/contact.html" || (candidate && path == "web/src/lib/private-contact/Contact.svelte")
		if !allowed {
			return Source{}, errRefused
		}
		entry, err := git(ctx, repository, "ls-tree", id, "--", path)
		if err != nil || !strings.HasPrefix(string(entry), "100644 blob ") || !strings.HasSuffix(string(entry), "\t"+path+"\n") {
			return Source{}, errRefused
		}
	}
	tree, err := git(ctx, repository, "rev-parse", "--verify", id+"^{tree}")
	value := strings.TrimSuffix(string(tree), "\n")
	if err != nil || !commitID.MatchString(value) {
		return Source{}, errRefused
	}
	return Source{Commit: id, Tree: value, Changed: paths}, nil
}

func hasDigest(ctx context.Context, repository, id, path, want string) bool {
	data, err := git(ctx, repository, "show", id+":"+path)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == want
}

// A plausible partial read never survives an unsuccessful command or overflow.
func git(ctx context.Context, repository string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects", "-c", "core.fsmonitor=false", "-C", repository}, args...)...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0"}
	output := &boundedOutput{}
	command.Stdout, command.Stderr = output, io.Discard
	if err := command.Run(); err != nil {
		return nil, errRefused
	}
	return output.buffer.Bytes(), nil
}

type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > 2*1024*1024-b.buffer.Len() {
		return 0, errRefused
	}
	return b.buffer.Write(data)
}
