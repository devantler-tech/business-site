package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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
	icons, err := run(2*time.Minute, "go", "run", filepath.Join(repo, "scripts/zitadel-login-icons.go"), "--inventory", "--build", build, "--notices", filepath.Join(repo, "third_party/zitadel-login-icons"))
	if err != nil {
		return fmt.Errorf("completed icon verification failed: %w", err)
	}
	stored, err = imageRead(build, "login-icons-inventory.json", 4<<20)
	if err != nil || !bytes.Equal(icons, stored) {
		return fmt.Errorf("completed icon inventory differs")
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
	cleaned = true
	if err = cleanup(); err != nil {
		return err
	}
	receipt := struct {
		SchemaVersion       int         `json:"schemaVersion"`
		Profile             string      `json:"profile"`
		UpstreamCommit      string      `json:"upstreamCommit"`
		Base                string      `json:"base"`
		LocalImageID        string      `json:"localImageId"`
		ContextSHA256       string      `json:"contextSha256"`
		DockerfileSHA256    string      `json:"dockerfileSha256"`
		PackageSourceSHA256 string      `json:"packageSourceSha256"`
		SmokeSourceSHA256   string      `json:"smokeSourceSha256"`
		Files               []imageFile `json:"files"`
		VersionOutput       string      `json:"versionOutput"`
		ConfiguredUser      string      `json:"configuredUser"`
		ReadOnlyRoot        bool        `json:"readOnlyRoot"`
		Network             string      `json:"network"`
		CapabilitiesDropped []string    `json:"capabilitiesDropped"`
		SecurityOptions     []string    `json:"securityOptions"`
		PIDLimit            int64       `json:"pidLimit"`
		MemoryLimitBytes    int64       `json:"memoryLimitBytes"`
		NanoCPUs            int64       `json:"nanoCPUs"`
		ExitCode            int         `json:"exitCode"`
		Cleanup             string      `json:"cleanup"`
		ImageAdmission      string      `json:"imageAdmission"`
		LicenseClosure      string      `json:"licenseClosure"`
		Reproducible        bool        `json:"reproducible"`
	}{1, "cli-version-smoke", imagePin, imageBase, i.ID, seal, imageHash(recipe), imageHash(packageSource), imageHash(smokeSource), manifest.Files, string(version), observed.Config.User, true, "none", observed.HostConfig.CapDrop, observed.HostConfig.SecurityOpt, observed.HostConfig.PidsLimit, observed.HostConfig.Memory, observed.HostConfig.NanoCpus, 0, "complete", "unknown", "unknown", false}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "image-smoke.json", append(b, '\n'), 0444)
}
