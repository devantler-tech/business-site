package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type imageCommand func(time.Duration, string, ...string) ([]byte, error)

type imageLimitedBuffer struct{ bytes.Buffer }

func (b *imageLimitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8<<20 {
		return 0, fmt.Errorf("command output exceeds bound")
	}
	return b.Buffer.Write(p)
}
func imageExecute(duration time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var output imageLimitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.Bytes(), fmt.Errorf("command deadline exceeded")
	}
	return output.Bytes(), err
}

type imageConfig struct {
	User, Image, WorkingDir string
	Entrypoint, Cmd         []string
	Env                     []string
	Labels                  map[string]string
}
type imageInspect struct {
	ID, Os, Architecture string
	Config               imageConfig
	RootFS               imageRootFS
}

type imageRootFS struct {
	Type   string   `json:"type"`
	Layers []string `json:"layers"`
}

// These uncompressed DiffIDs come from the original amd64 config blob
// 55a757dc8ab862a4eb634c434261dcd26f17c186b54f7bce845404a335456110,
// selected by the immutable base index. They are not registry layer digests.
var imageBaseDiffIDs = []string{
	"sha256:9486261b5a13f63ca6904b36721527ac8f19df53d300560138d69ae9b4c203de",
	"sha256:621c35e751a51a9a9dc3e80aa0b7fe8be2a93402ea6ccd307d30852cd7776cda",
	"sha256:763a77745f27b86cf26b97887b6842d83720e63b1c48848c0ba9ee54e351d63a",
	"sha256:f0d0eff4c2312a7bd6e21797bdfea5eb35f60e76b555ce2b22794424af2cad36",
	"sha256:275a30dd8ce958b21daa9ad962c6fbc09f98306ee2f486b65c9075dc257b1412",
	"sha256:4d049f83d9cf21d1f5cc0e11deaf36df02790d0e60c1a3829538fb4b61685368",
	"sha256:af5aa97ebe6ce1604747ec1e21af7136ded391bcabe4acef882e718a87c86bcc",
	"sha256:6f1cdceb6a3146f0ccb986521156bef8a422cdbb0863396f7f751f575ba308f4",
	"sha256:bd3cdfae1d3fdd83a2231d608969b38b82349777c2fff9a7c12d54f8ac5c9b38",
	"sha256:4cde6b0bb6f50a5f255eef7b2a42162c661cf776b803225dcac9a659e396bb6b",
	"sha256:ad51d0769d16ba578106a177987dfe3d2e02c1668c852b795b2f6b024068242a",
	"sha256:187cfc6d1e3e8a40a5e64653bcd3239c140807dcf1c09e48021178705a5a6139",
	"sha256:f2392a9484bf6b18f06beaf705b994e31dbb2716c99cf7d03c9c495cd107f364",
}

// Original Debian copyright bodies and the referenced complete GPL-2 text,
// observed in the frozen base layers. They remain separate from /app's notices.
type imageBaseNotice struct {
	ImagePath string    `json:"imagePath"`
	File      imageFile `json:"file"`
}

var imageBaseNotices = []imageBaseNotice{
	{"/usr/share/doc/base-files/copyright", imageFile{Path: "base-notices/base-files-copyright.txt", Bytes: 1696, SHA256: "c84c664bce3a83eaa327bb9e354c0df588bc535b5e95474aab7ca3cce74937cd", Mode: 0644}},
	{"/usr/share/doc/netbase/copyright", imageFile{Path: "base-notices/netbase-copyright.txt", Bytes: 1234, SHA256: "795b66147ea5ad692991caa7008ece551fb0fa88b9c53656223bd1518dc58ab2", Mode: 0644}},
	{"/usr/share/doc/tzdata/copyright", imageFile{Path: "base-notices/tzdata-copyright.txt", Bytes: 375, SHA256: "cb61132bc0fc7b26ef5a82ee18b2fb644a1362f4f286ed980ff22e408471f59a", Mode: 0644}},
	{"/usr/share/doc/tzdata-legacy/copyright", imageFile{Path: "base-notices/tzdata-legacy-copyright.txt", Bytes: 375, SHA256: "cb61132bc0fc7b26ef5a82ee18b2fb644a1362f4f286ed980ff22e408471f59a", Mode: 0644}},
	{"/usr/share/doc/media-types/copyright", imageFile{Path: "base-notices/media-types-copyright.txt", Bytes: 268, SHA256: "5ded7c53199b6a1d089b0b7e231d77e5c84081c7156b372b3f11ad9c1c2c5cc7", Mode: 0644}},
	{"/usr/share/doc/ca-certificates/copyright", imageFile{Path: "base-notices/ca-certificates-copyright.txt", Bytes: 18940, SHA256: "e85e1bcad3a915dc7e6f41412bc5bdeba275cadd817896ea0451f2140a93967c", Mode: 0555}},
	{"/usr/share/common-licenses/GPL-2", imageFile{Path: "base-notices/GPL-2.txt", Bytes: 17984, SHA256: "edaef632cbb643e4e7a221717a6c441a4c1a7c918e6e4d56debc3d8739b233f6", Mode: 0644}},
}

func imageVerifyBaseNotices(output string) error {
	entries, err := os.ReadDir(filepath.Join(output, "base-notices"))
	if err != nil || len(entries) != len(imageBaseNotices) {
		return fmt.Errorf("complete base notice copies required")
	}
	for _, notice := range imageBaseNotices {
		b, err := imageRead(output, notice.File.Path, 65536)
		st, stErr := os.Lstat(filepath.Join(output, notice.File.Path))
		if err != nil || stErr != nil || len(b) != notice.File.Bytes || imageHash(b) != notice.File.SHA256 || !st.Mode().IsRegular() || uint32(st.Mode().Perm()) != notice.File.Mode {
			return fmt.Errorf("actual base notice differs: %s", notice.ImagePath)
		}
	}
	return nil
}
func imageCaptureBaseNotices(output, container string, run imageCommand) error {
	if err := os.Mkdir(filepath.Join(output, "base-notices"), 0700); err != nil {
		return err
	}
	for _, notice := range imageBaseNotices {
		if _, err := run(30*time.Second, "docker", "cp", container+":"+notice.ImagePath, filepath.Join(output, notice.File.Path)); err != nil {
			return fmt.Errorf("actual base notice copy failed: %w", err)
		}
	}
	return imageVerifyBaseNotices(output)
}

func imageBaseRootFSValid(root imageRootFS) bool {
	// The frozen Dockerfile has WORKDIR plus two distinct COPY instructions.
	// Each adds one nonempty filesystem layer; configuration-only steps add none.
	if root.Type != "layers" || len(root.Layers) != len(imageBaseDiffIDs)+3 ||
		!reflect.DeepEqual(root.Layers[:len(imageBaseDiffIDs)], imageBaseDiffIDs) {
		return false
	}
	for _, layer := range root.Layers[len(imageBaseDiffIDs):] {
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(layer) {
			return false
		}
	}
	return true
}

type imageContainer struct {
	ID, Image  string
	Config     imageConfig
	HostConfig struct {
		ReadonlyRootfs, Privileged, PublishAllPorts bool
		CapDrop, CapAdd, SecurityOpt                []string
		NetworkMode, PidMode, IpcMode               string
		PidsLimit, Memory, NanoCpus                 int64
		Binds, Devices                              []json.RawMessage
		Tmpfs                                       map[string]string
		PortBindings                                map[string]json.RawMessage
	}
	State struct {
		Status                   string
		Running, Dead, OOMKilled bool
		ExitCode                 int
		Pid                      int
	}
	NetworkSettings struct {
		Networks map[string]struct{ NetworkID, IPAddress string }
	}
	Mounts []json.RawMessage
}

func imageSafeConfig(c imageConfig) bool {
	return c.User == "65532:65532" && reflect.DeepEqual(c.Entrypoint, []string{"/app/zitadel"}) && reflect.DeepEqual(c.Cmd, []string{"--version"})
}
func imageInspectContainer(raw []byte, id, image, status string) (imageContainer, error) {
	var all []imageContainer
	if err := imageDecode(raw, &all); err != nil || len(all) != 1 {
		return imageContainer{}, fmt.Errorf("invalid container inspection")
	}
	c := all[0]
	h := c.HostConfig
	if c.ID != id || c.Image != image || c.Config.Image != image || !imageSafeConfig(c.Config) || !h.ReadonlyRootfs || h.Privileged || !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || len(h.CapAdd) != 0 || !reflect.DeepEqual(h.SecurityOpt, []string{"no-new-privileges"}) || h.NetworkMode != "none" || h.PidsLimit != 32 || h.Memory != 268435456 || h.NanoCpus != 1000000000 || len(h.Binds) != 0 || len(h.Devices) != 0 || len(h.PortBindings) != 0 || len(c.Mounts) != 0 || h.PidMode != "" || h.IpcMode != "private" || c.State.Status != status || c.State.Running || c.State.Dead || c.State.OOMKilled || c.State.ExitCode != 0 {
		return imageContainer{}, fmt.Errorf("container restrictions or state differ")
	}
	return c, nil
}

// Preserve the exact exercised config ID before its owner-bound cleanup. This
// seals an opaque local Docker-save handoff, not a registry manifest or admission.
// A publisher must independently load it and verify that ID and its payload.
func imageSaveCandidate(output, imageID string, run imageCommand) (candidate imageFile, result error) {
	const name = "candidate-image.tar"
	root, err := os.OpenRoot(output)
	if err != nil {
		return candidate, err
	}
	defer func() {
		if err := root.Close(); err != nil && result == nil {
			result = err
		}
	}()
	if _, err = root.Lstat(name); !os.IsNotExist(err) {
		return candidate, fmt.Errorf("candidate destination already exists or is unreadable")
	}
	if _, err = run(3*time.Minute, "docker", "image", "save", "--output", filepath.Join(output, name), imageID); err != nil {
		return candidate, fmt.Errorf("tested candidate export failed: %w", err)
	}
	return imageSealCandidate(output)
}

func imageSealCandidate(output string) (candidate imageFile, result error) {
	const name = "candidate-image.tar"
	const limit int64 = 1 << 30
	root, err := os.OpenRoot(output)
	if err != nil {
		return candidate, err
	}
	defer func() {
		if err := root.Close(); err != nil && result == nil {
			result = err
		}
	}()
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return candidate, err
	}
	defer func() {
		if err := f.Close(); err != nil && result == nil {
			result = err
		}
	}()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit {
		return candidate, fmt.Errorf("invalid candidate size or type")
	}
	linkCount, ok := before.Sys().(*syscall.Stat_t)
	if !ok || linkCount.Nlink != 1 {
		return candidate, fmt.Errorf("candidate must have one owned pathname")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, limit+1))
	if err != nil || n != before.Size() {
		return candidate, fmt.Errorf("incomplete candidate read")
	}
	if err = f.Chmod(0444); err != nil {
		return candidate, err
	}
	after, err := f.Stat()
	entry, entryErr := root.Lstat(name)
	if err != nil || entryErr != nil || !os.SameFile(before, after) || !os.SameFile(after, entry) || !entry.Mode().IsRegular() || after.Size() != n || entry.Size() != n || !after.ModTime().Equal(before.ModTime()) || entry.Mode().Perm() != 0444 {
		return candidate, fmt.Errorf("candidate changed while sealing")
	}
	linkCount, ok = after.Sys().(*syscall.Stat_t)
	if !ok || linkCount.Nlink != 1 {
		return candidate, fmt.Errorf("candidate gained an unowned pathname")
	}
	return imageFile{Path: name, Bytes: int(n), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Mode: 0444}, nil
}

func imageSmoke(build, repo, output string, run imageCommand, probes ...func(string, string) error) (result error) {
	var err error
	output, err = imageOutputPath(build, repo, output)
	if err != nil {
		return err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	goRoot, err := run(30*time.Second, "go", "env", "GOROOT")
	if err != nil || strings.TrimSpace(string(goRoot)) == "" {
		return fmt.Errorf("go distribution unavailable")
	}
	console, err := run(2*time.Minute, "go", "run", filepath.Join(repo, "scripts/zitadel-console-notices.go"), "--verify", "--build", build, "--recipe", filepath.Join(repo, "scripts/build-zitadel-linux.sh"), "--supplement", filepath.Join(repo, "third_party/zitadel-console-notices"), "--toolchain", strings.TrimSpace(string(goRoot)), "--source-license", "--modules", filepath.Join(repo, "third_party/zitadel-go-module-notices"))
	if err != nil {
		return fmt.Errorf("completed notice verification failed: %w", err)
	}
	stored, err := imageRead(build, "console-notices-inventory.json", 4<<20)
	if err != nil || !bytes.Equal(console, stored) {
		return fmt.Errorf("completed console inventory differs")
	}
	icons, err := run(2*time.Minute, "go", "run", filepath.Join(repo, "scripts/zitadel-login-icons.go"), filepath.Join(repo, "scripts/zitadel-text-fonts.go"), "--inventory", "--build", build, "--notices", filepath.Join(repo, "third_party/zitadel-login-icons"))
	if err != nil {
		return fmt.Errorf("completed icon verification failed: %w", err)
	}
	stored, err = imageRead(build, "login-icons-inventory.json", 4<<20)
	if err != nil || !bytes.Equal(icons, stored) {
		return fmt.Errorf("completed icon inventory differs")
	}
	fonts, err := run(2*time.Minute, "go", "run", filepath.Join(repo, "scripts/zitadel-login-icons.go"), filepath.Join(repo, "scripts/zitadel-text-fonts.go"), "--text-fonts", "--verify", "--build", build, "--recipe", filepath.Join(repo, "scripts/build-zitadel-linux.sh"), "--notices", filepath.Join(repo, "third_party/zitadel-text-fonts"))
	if err != nil {
		return fmt.Errorf("completed text-font verification failed: %w", err)
	}
	stored, err = imageRead(build, "text-font-notices-inventory.json", 4<<20)
	if err != nil || !bytes.Equal(fonts, stored) {
		return fmt.Errorf("completed text-font inventory differs")
	}
	copied, err := run(2*time.Minute, "go", "run", filepath.Join(repo, "scripts/zitadel-login-icons.go"), filepath.Join(repo, "scripts/zitadel-text-fonts.go"), filepath.Join(repo, "scripts/zitadel-copied-assets.go"), "--copied-assets", "--verify", "--build", build, "--recipe", filepath.Join(repo, "scripts/build-zitadel-linux.sh"), "--notices", filepath.Join(repo, "third_party/zitadel-copied-assets"))
	if err != nil {
		return fmt.Errorf("completed copied-asset verification failed: %w", err)
	}
	stored, err = imageRead(build, "copied-asset-notices-inventory.json", 4<<20)
	if err != nil || !bytes.Equal(copied, stored) {
		return fmt.Errorf("completed copied-asset inventory differs")
	}
	contextDir := filepath.Join(output, "context")
	seal, err := imagePackage(build, repo, contextDir)
	if err != nil {
		return err
	}
	recipe, err := imageRead(repo, "Dockerfile.zitadel", 65536)
	if err != nil {
		return err
	}
	packageSource, err := imageRead(repo, "scripts/zitadel-image-package.go", 1<<20)
	if err != nil {
		return err
	}
	smokeSource, err := imageRead(repo, "scripts/zitadel-image-smoke.go", 1<<20)
	if err != nil {
		return err
	}
	info, err := run(30*time.Second, "docker", "info", "--format", "{{json .}}")
	if err != nil {
		return fmt.Errorf("docker engine unavailable")
	}
	var engine struct {
		OSType, Architecture string
		SecurityOptions      []string
	}
	if err = imageDecode(info, &engine); err != nil || engine.OSType != "linux" || (engine.Architecture != "x86_64" && engine.Architecture != "amd64") {
		return fmt.Errorf("native Linux amd64 Docker engine required")
	}
	seccomp := false
	for _, s := range engine.SecurityOptions {
		if s == "name=seccomp,profile=builtin" {
			seccomp = true
		}
	}
	if !seccomp {
		return fmt.Errorf("docker builtin seccomp not confirmed")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	owner := hex.EncodeToString(nonce[:])
	ownerLabel := "org.devantler-tech.identity-smoke-owner"
	container := ""
	ownedContainer, ownedImage := "", ""
	cleaned := false
	cleanup := func() error {
		var failures []string
		if ownedContainer != "" {
			if _, e := run(30*time.Second, "docker", "rm", "--force", ownedContainer); e != nil {
				failures = append(failures, "owned container")
			}
		}
		if ownedImage != "" {
			if _, e := run(30*time.Second, "docker", "image", "rm", ownedImage); e != nil {
				failures = append(failures, "owned image")
			}
		}
		if len(failures) != 0 {
			return fmt.Errorf("cleanup failed: %s", strings.Join(failures, ", "))
		}
		return nil
	}
	defer func() {
		if !cleaned {
			if e := cleanup(); e != nil {
				if result == nil {
					result = e
				} else {
					result = fmt.Errorf("%w; %s", result, e)
				}
			}
		}
	}()
	if _, err = run(3*time.Minute, "docker", "build", "--network=none", "--pull=false", "--platform=linux/amd64", "--file", filepath.Join(repo, "Dockerfile.zitadel"), "--label", ownerLabel+"="+owner, "--iidfile", filepath.Join(output, "image.id"), contextDir); err != nil {
		return fmt.Errorf("local image build failed: %w", err)
	}
	idBytes, err := imageRead(output, "image.id", 128)
	if err != nil {
		return err
	}
	imageID := strings.TrimSpace(string(idBytes))
	digestPattern := regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	if !digestPattern.MatchString(imageID) {
		return fmt.Errorf("invalid local image identity")
	}
	inspection, err := run(30*time.Second, "docker", "image", "inspect", imageID)
	if err != nil {
		return err
	}
	var images []imageInspect
	if err = imageDecode(inspection, &images); err != nil || len(images) != 1 {
		return fmt.Errorf("invalid local image inspection")
	}
	i := images[0]
	if i.ID != imageID || i.Config.Labels[ownerLabel] != owner {
		return fmt.Errorf("local image ownership not confirmed")
	}
	ownedImage = i.ID
	if i.Os != "linux" || i.Architecture != "amd64" || i.Config.WorkingDir != "/app" || !imageSafeConfig(i.Config) {
		return fmt.Errorf("local image configuration differs")
	}
	if !imageBaseRootFSValid(i.RootFS) {
		return fmt.Errorf("local image base filesystem differs (type=%q layers=%d, expected frozen base plus WORKDIR and two COPY layers)", i.RootFS.Type, len(i.RootFS.Layers))
	}
	created, err := run(30*time.Second, "docker", "create", "--label", ownerLabel+"="+owner, "--pull=never", "--platform=linux/amd64", "--network=none", "--user=65532:65532", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=256m", "--cpus=1", i.ID)
	if err != nil {
		return err
	}
	container = strings.TrimSpace(string(created))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(container) {
		return fmt.Errorf("invalid created container identity")
	}
	before, err := run(30*time.Second, "docker", "inspect", container)
	if err != nil {
		return err
	}
	var candidates []imageContainer
	if err = imageDecode(before, &candidates); err != nil || len(candidates) != 1 || candidates[0].ID != container || candidates[0].Image != i.ID || candidates[0].Config.Labels[ownerLabel] != owner {
		return fmt.Errorf("local container ownership not confirmed")
	}
	ownedContainer = container
	if _, err = imageInspectContainer(before, container, i.ID, "created"); err != nil {
		return err
	}
	version, err := run(30*time.Second, "docker", "start", "--attach", container)
	if err != nil || !bytes.Equal(version, []byte("zitadel version 4.20.0\n")) {
		return fmt.Errorf("actual restricted CLI execution failed")
	}
	after, err := run(30*time.Second, "docker", "inspect", container)
	if err != nil {
		return err
	}
	observed, err := imageInspectContainer(after, container, i.ID, "exited")
	if err != nil {
		return err
	}
	if err = imageCaptureBaseNotices(output, container, run); err != nil {
		return err
	}
	extracted := filepath.Join(output, "extracted")
	if err = os.Mkdir(extracted, 0700); err != nil {
		return err
	}
	if _, err = run(time.Minute, "docker", "cp", container+":/app/.", extracted); err != nil {
		return err
	}
	if err = imageVerifyContext(extracted, seal); err != nil {
		return fmt.Errorf("actual image payload differs: %w", err)
	}
	if err = imageVerifyContext(contextDir, seal); err != nil {
		return err
	}
	manifestBytes, err := imageRead(contextDir, "package-manifest.json", 1<<20)
	if err != nil {
		return err
	}
	var manifest imageManifest
	if err = imageDecode(manifestBytes, &manifest); err != nil {
		return err
	}
	afterRecipe, err := imageRead(repo, "Dockerfile.zitadel", 65536)
	if err != nil || !bytes.Equal(recipe, afterRecipe) {
		return fmt.Errorf("image recipe changed during build")
	}
	for _, probe := range probes {
		if err = probe(i.ID, owner); err != nil {
			return err
		}
	}
	if err = imageVerifyBaseNotices(output); err != nil {
		return err
	}
	candidate, err := imageSaveCandidate(output, i.ID, run)
	if err != nil {
		return err
	}
	cleaned = true
	if err = cleanup(); err != nil {
		return err
	}
	ownedContainer, ownedImage = "", ""
	cleaned = false
	absent, err := run(30*time.Second, "docker", "image", "ls", "--all", "--no-trunc", "--quiet", "--filter", "label="+ownerLabel+"="+owner)
	if err != nil || len(bytes.TrimSpace(absent)) != 0 {
		return fmt.Errorf("original owned image absence not confirmed")
	}
	beforeLoad, err := imageSealCandidate(output)
	if err != nil || beforeLoad != candidate {
		return fmt.Errorf("candidate archive changed before reload")
	}
	_, loadErr := run(3*time.Minute, "docker", "image", "load", "--input", filepath.Join(output, candidate.Path))
	// Even a failed load can leave this owned image behind. Inspect before
	// assigning cleanup authority, and never infer success from CLI prose.
	loadedRaw, inspectErr := run(30*time.Second, "docker", "image", "inspect", i.ID)
	var loaded []imageInspect
	if inspectErr != nil || imageDecode(loadedRaw, &loaded) != nil || len(loaded) != 1 || loaded[0].ID != i.ID || loaded[0].Config.Labels[ownerLabel] != owner {
		return fmt.Errorf("reloaded candidate ownership or identity differs")
	}
	ownedImage = loaded[0].ID
	if loadErr != nil {
		return fmt.Errorf("candidate reload failed: %w", loadErr)
	}
	if !reflect.DeepEqual(loaded[0], i) {
		return fmt.Errorf("reloaded candidate configuration differs")
	}
	readback, err := imageSealCandidate(output)
	if err != nil || readback != candidate {
		return fmt.Errorf("candidate archive changed after reload")
	}
	cleaned = true
	if err = cleanup(); err != nil {
		return err
	}
	if err = imageVerifyBaseNotices(output); err != nil {
		return err
	}
	receipt := struct {
		SchemaVersion       int               `json:"schemaVersion"`
		Profile             string            `json:"profile"`
		UpstreamCommit      string            `json:"upstreamCommit"`
		Base                string            `json:"base"`
		BaseRootFS          imageRootFS       `json:"baseRootFS"`
		BaseNotices         []imageBaseNotice `json:"baseNotices"`
		LocalImageID        string            `json:"localImageId"`
		CandidateArchive    imageFile         `json:"candidateArchive"`
		CandidateReloaded   bool              `json:"candidateReloaded"`
		ContextSHA256       string            `json:"contextSha256"`
		DockerfileSHA256    string            `json:"dockerfileSha256"`
		PackageSourceSHA256 string            `json:"packageSourceSha256"`
		SmokeSourceSHA256   string            `json:"smokeSourceSha256"`
		Files               []imageFile       `json:"files"`
		VersionOutput       string            `json:"versionOutput"`
		ConfiguredUser      string            `json:"configuredUser"`
		ReadOnlyRoot        bool              `json:"readOnlyRoot"`
		Network             string            `json:"network"`
		CapabilitiesDropped []string          `json:"capabilitiesDropped"`
		SecurityOptions     []string          `json:"securityOptions"`
		PIDLimit            int64             `json:"pidLimit"`
		MemoryLimitBytes    int64             `json:"memoryLimitBytes"`
		NanoCPUs            int64             `json:"nanoCPUs"`
		ExitCode            int               `json:"exitCode"`
		Cleanup             string            `json:"cleanup"`
		ImageAdmission      string            `json:"imageAdmission"`
		LicenseClosure      string            `json:"licenseClosure"`
		Reproducible        bool              `json:"reproducible"`
	}{1, "cli-version-smoke", imagePin, imageBase, i.RootFS, imageBaseNotices, i.ID, candidate, true, seal, imageHash(recipe), imageHash(packageSource), imageHash(smokeSource), manifest.Files, string(version), observed.Config.User, true, "none", observed.HostConfig.CapDrop, observed.HostConfig.SecurityOpt, observed.HostConfig.PidsLimit, observed.HostConfig.Memory, observed.HostConfig.NanoCpus, 0, "complete", "unknown", "unknown", false}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "image-smoke.json", append(b, '\n'), 0444)
}

// imageVerifySaved inspects the already exercised original. It never compiles,
// builds, starts or exports an identity image. A successful result leaves only
// the newly loaded exact image for the caller's explicit publication operation.
func imageVerifySaved(native, notices, output string, run imageCommand) (result error) {
	output, err := imageOutputPath(filepath.Join(native, "zitadel-image"), notices, output)
	if err != nil {
		return err
	}
	if _, err = imageOutputPath(filepath.Join(native, "zitadel-build"), notices, output); err != nil {
		return err
	}
	raw, err := imageRead(filepath.Join(native, "zitadel-image"), "image-smoke.json", 8<<20)
	if err != nil {
		return err
	}
	var original struct {
		SchemaVersion                 int `json:"schemaVersion"`
		Profile, UpstreamCommit, Base string
		LocalImageID                  string      `json:"localImageId"`
		CandidateArchive              imageFile   `json:"candidateArchive"`
		CandidateReloaded             bool        `json:"candidateReloaded"`
		ContextSHA256                 string      `json:"contextSha256"`
		BaseRootFS                    imageRootFS `json:"baseRootFS"`
	}
	if err = imageDecode(raw, &original); err != nil || original.SchemaVersion != 1 || original.Profile != "cli-version-smoke" || original.UpstreamCommit != imagePin || original.Base != imageBase || !original.CandidateReloaded || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(original.LocalImageID) || !imageBaseRootFSValid(original.BaseRootFS) {
		return fmt.Errorf("original saved-image receipt differs")
	}
	candidate, err := imageSealCandidate(filepath.Join(native, "zitadel-image"))
	if err != nil || candidate != original.CandidateArchive {
		return fmt.Errorf("original saved-image archive differs")
	}
	// A failed inspection is not evidence of absence. The complete, successful
	// ID listing must show no image with the expected immutable config digest.
	listing, err := run(30*time.Second, "docker", "image", "ls", "--all", "--no-trunc", "--quiet")
	if err != nil {
		return fmt.Errorf("saved-image absence query failed")
	}
	for _, id := range strings.Fields(string(listing)) {
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(id) || id == original.LocalImageID {
			return fmt.Errorf("original saved-image ID already exists or listing is malformed")
		}
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	owner := hex.EncodeToString(nonce[:])
	label := "org.devantler-tech.identity-smoke-owner"
	ownedImage, ownedContainer := "", ""
	defer func() {
		if ownedContainer != "" {
			if _, e := run(30*time.Second, "docker", "rm", ownedContainer); e != nil {
				result = fmt.Errorf("%v; owned saved-image container cleanup failed", result)
			}
		}
		if result != nil && ownedImage != "" {
			if _, e := run(30*time.Second, "docker", "image", "rm", ownedImage); e != nil {
				result = fmt.Errorf("%w; owned saved-image cleanup failed", result)
			}
		}
	}()
	_, loadErr := run(3*time.Minute, "docker", "image", "load", "--input", filepath.Join(native, "zitadel-image/candidate-image.tar"))
	inspection, inspectErr := run(30*time.Second, "docker", "image", "inspect", original.LocalImageID)
	var images []imageInspect
	if inspectErr != nil || imageDecode(inspection, &images) != nil || len(images) != 1 || images[0].ID != original.LocalImageID {
		return fmt.Errorf("loaded original config identity differs")
	}
	// This ID was positively absent before this load; no preexisting image is
	// adopted even when it happens to carry an earlier invocation's owner label.
	ownedImage = images[0].ID
	if loadErr != nil {
		return fmt.Errorf("saved-image load failed: %w", loadErr)
	}
	loaded := images[0]
	if loaded.Os != "linux" || loaded.Architecture != "amd64" || loaded.Config.WorkingDir != "/app" || !imageSafeConfig(loaded.Config) || !reflect.DeepEqual(loaded.RootFS, original.BaseRootFS) {
		return fmt.Errorf("loaded original image configuration differs")
	}
	created, err := run(30*time.Second, "docker", "create", "--label", label+"="+owner, "--pull=never", "--platform=linux/amd64", "--network=none", "--user=65532:65532", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=32", "--memory=256m", "--cpus=1", loaded.ID)
	if err != nil {
		return err
	}
	container := strings.TrimSpace(string(created))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(container) {
		return fmt.Errorf("invalid saved-image container identity")
	}
	inspection, err = run(30*time.Second, "docker", "inspect", container)
	if err != nil {
		return err
	}
	var all []imageContainer
	if imageDecode(inspection, &all) != nil || len(all) != 1 || all[0].ID != container || all[0].Image != loaded.ID || all[0].Config.Labels[label] != owner {
		return fmt.Errorf("saved-image container ownership differs")
	}
	ownedContainer = container
	if _, err = imageInspectContainer(inspection, container, loaded.ID, "created"); err != nil {
		return err
	}
	extracted := filepath.Join(output, "extracted")
	if err = os.Mkdir(extracted, 0700); err != nil {
		return err
	}
	if _, err = run(time.Minute, "docker", "cp", container+":/app/.", extracted); err != nil {
		return err
	}
	if err = imageVerifyContext(extracted, original.ContextSHA256); err != nil {
		return fmt.Errorf("saved original payload differs: %w", err)
	}
	if err = imageCaptureBaseNotices(output, container, run); err != nil {
		return err
	}
	// Reconstruct only a verification view of the copied payload. This does not
	// recreate a completed build or manufacture producer notice inventories.
	view := filepath.Join(output, "view")
	if err = os.Mkdir(view, 0700); err != nil {
		return err
	}
	for _, name := range []string{"build-receipt.json", "notices/console-notices-inventory.json", "notices/console-notices-supplement-inventory.json", "notices/login-icons-inventory.json", "notices/text-font-notices-inventory.json", "notices/copied-asset-notices-inventory.json", "notices/console-third-party-notices.txt", "notices/console-notices-supplement.txt", "notices/go-toolchain-notices.txt", "notices/zitadel-root-license.txt", "notices/go-module-notices.txt", "notices/text-font-notices.txt", "notices/copied-asset-notices.txt", "zitadel"} {
		b, e := imageRead(extracted, name, 512<<20)
		if e != nil {
			return e
		}
		target := filepath.Base(name)
		if name == "zitadel" {
			target = "service/zitadel"
		}
		if err = imageWrite(view, target, b, 0444); err != nil {
			return err
		}
	}
	expected, err := imageRead(filepath.Join(native, "zitadel-build"), "build-receipt.json", 8<<20)
	if err != nil {
		return err
	}
	actual, err := imageRead(extracted, "build-receipt.json", 8<<20)
	if err != nil || !bytes.Equal(actual, expected) {
		return fmt.Errorf("saved image build receipt differs from original CI")
	}
	seal, err := imagePackage(view, notices, filepath.Join(output, "verified-context"))
	if err != nil || seal != original.ContextSHA256 {
		return fmt.Errorf("saved image source/notice package differs: %v", err)
	}
	after, err := imageSealCandidate(filepath.Join(native, "zitadel-image"))
	if err != nil || after != candidate {
		return fmt.Errorf("saved archive changed during verification")
	}
	receipt := struct {
		SchemaVersion               int    `json:"schemaVersion"`
		Profile                     string `json:"profile"`
		LocalImageID                string `json:"localImageId"`
		ContextSHA256               string `json:"contextSha256"`
		ImageRetainedForPublication bool   `json:"imageRetainedForPublication"`
		ImagePublished              bool   `json:"imagePublished"`
		PortalActivated             bool   `json:"portalActivated"`
	}{1, "identity-saved-image-v1", loaded.ID, seal, true, false, false}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "saved-image.json", append(body, '\n'), 0444)
}
