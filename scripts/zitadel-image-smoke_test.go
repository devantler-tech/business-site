package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Process doubles test orchestration failures, never real container acceptance.
func imageBaseFixtureLayers(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/zitadel-source/base/config.json.raw")
	if err != nil || imageHash(raw) != "55a757dc8ab862a4eb634c434261dcd26f17c186b54f7bce845404a335456110" {
		t.Fatal("independent frozen base config differs")
	}
	var config struct {
		RootFS struct {
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || len(config.RootFS.DiffIDs) != 13 {
		t.Fatal("independent base RootFS observation incomplete")
	}
	return append(config.RootFS.DiffIDs, "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64), "sha256:"+strings.Repeat("e", 64))
}

func TestImageSmokeRejectsAnUnboundBaseFilesystem(t *testing.T) {
	for _, kind := range []string{"missing", "short", "substituted", "extra", "wrong-type", "old-single", "only-two", "bad-first-app", "bad-middle-app", "bad-last-app"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
			base := imageSmokeDouble(t, build, "")
			run := func(d time.Duration, name string, args ...string) ([]byte, error) {
				raw, err := base(d, name, args...)
				if err == nil && name == "docker" && args[0] == "image" && args[1] == "inspect" {
					var all []map[string]any
					if err := json.Unmarshal(raw, &all); err != nil {
						t.Fatal(err)
					}
					layers, fsType := imageBaseFixtureLayers(t), "layers"
					switch kind {
					case "missing":
						layers = nil
					case "short":
						layers = layers[:12]
					case "substituted":
						layers[0] = "sha256:" + strings.Repeat("d", 64)
					case "extra":
						layers = append(layers, "sha256:"+strings.Repeat("e", 64))
					case "wrong-type":
						fsType = "other"
					case "old-single":
						layers = layers[:14]
					case "only-two":
						layers = layers[:15]
					case "bad-first-app":
						layers[13] = "invalid"
					case "bad-middle-app":
						layers[14] = "invalid"
					case "bad-last-app":
						layers[15] = "invalid"
					}
					all[0]["RootFS"] = map[string]any{"Type": fsType, "Layers": layers}
					return imageTestJSON(t, all), nil
				}
				return raw, err
			}
			if err := imageSmoke(build, repo, filepath.Join(t.TempDir(), "smoke"), run); err == nil {
				t.Fatal("image with unbound base filesystem was accepted")
			}
		})
	}
}

func imageSmokeDouble(t *testing.T, build string, kind string) imageCommand {
	t.Helper()
	started := false
	owner := ""
	return func(_ time.Duration, name string, args ...string) ([]byte, error) {
		if name == "go" {
			if args[0] == "env" {
				return []byte("/fixture/go\n"), nil
			}
			p := "console-notices-inventory.json"
			if strings.Contains(strings.Join(args, " "), "zitadel-login-icons.go") {
				p = "login-icons-inventory.json"
			}
			if strings.Contains(strings.Join(args, " "), "--text-fonts") {
				p = "text-font-notices-inventory.json"
			}
			if strings.Contains(strings.Join(args, " "), "--copied-assets") {
				if kind == "copied-verification-error" {
					return []byte(`{"plausible":true}`), fmt.Errorf("copied verification failed after output")
				}
				p = "copied-asset-notices-inventory.json"
			}
			b, err := os.ReadFile(filepath.Join(build, p))
			if kind == "verification-error" {
				return b, fmt.Errorf("failed after plausible inventory")
			}
			return b, err
		}
		if name != "docker" {
			t.Fatalf("unexpected executable: %s", name)
		}
		command := strings.Join(args, " ")
		switch {
		case args[0] == "info":
			security := []string{"name=seccomp,profile=builtin"}
			if kind == "seccomp" {
				security = nil
			}
			return imageTestJSON(t, map[string]any{"OSType": "linux", "Architecture": "x86_64", "SecurityOptions": security}), nil
		case args[0] == "build":
			for i, arg := range args {
				if arg == "--label" {
					owner = strings.TrimPrefix(args[i+1], "org.devantler-tech.identity-smoke-owner=")
				}
				if arg == "--iidfile" {
					imageTestWrite(t, filepath.Dir(args[i+1]), filepath.Base(args[i+1]), []byte("sha256:"+strings.Repeat("b", 64)+"\n"))
				}
			}
			if kind == "build-error" {
				return []byte("successfully built plausible-image"), fmt.Errorf("failed build")
			}
			return nil, nil
		case args[0] == "image" && args[1] == "inspect":
			user := "65532:65532"
			if kind == "image-user" {
				user = "0"
			}
			arch := "amd64"
			if kind == "architecture" {
				arch = "arm64"
			}
			return imageTestJSON(t, []any{map[string]any{"Id": "sha256:" + strings.Repeat("b", 64), "Os": "linux", "Architecture": arch, "RootFS": map[string]any{"Type": "layers", "Layers": imageBaseFixtureLayers(t)}, "Config": map[string]any{"User": user, "Entrypoint": []string{"/app/zitadel"}, "Cmd": []string{"--version"}, "WorkingDir": "/app", "Labels": map[string]string{"org.devantler-tech.identity-smoke-owner": owner}}}}), nil
		case args[0] == "create":
			for _, flag := range []string{"--read-only", "--network=none", "--user=65532:65532", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pull=never", "--platform=linux/amd64", "--pids-limit=32", "--memory=256m", "--cpus=1"} {
				if !strings.Contains(command, flag) {
					t.Fatalf("restriction not passed: %s", flag)
				}
			}
			return []byte(strings.Repeat("a", 64) + "\n"), nil
		case args[0] == "inspect":
			host := map[string]any{"ReadonlyRootfs": true, "Privileged": false, "CapDrop": []string{"ALL"}, "CapAdd": nil, "SecurityOpt": []string{"no-new-privileges"}, "NetworkMode": "none", "PidsLimit": 32, "Memory": 268435456, "NanoCpus": 1000000000, "Binds": nil, "PortBindings": nil, "Devices": nil, "PidMode": "", "IpcMode": "private"}
			status := "created"
			if started {
				status = "exited"
			}
			state := map[string]any{"Status": status, "Running": false, "Dead": false, "OOMKilled": false, "ExitCode": 0}
			config := map[string]any{"User": "65532:65532", "Image": "sha256:" + strings.Repeat("b", 64), "Entrypoint": []string{"/app/zitadel"}, "Cmd": []string{"--version"}}
			config["Labels"] = map[string]string{"org.devantler-tech.identity-smoke-owner": owner}
			mounts := []any{}
			switch kind {
			case "container-user":
				config["User"] = "0"
			case "writable":
				host["ReadonlyRootfs"] = false
			case "privileged":
				host["Privileged"] = true
			case "capability":
				host["CapAdd"] = []string{"SYS_ADMIN"}
			case "network":
				host["NetworkMode"] = "host"
			case "no-limits":
				host["Memory"] = 0
			case "mount":
				mounts = append(mounts, map[string]any{"Source": "fixture", "Destination": "/app"})
			case "oom":
				state["OOMKilled"] = true
			case "exit":
				state["ExitCode"] = 1
			}
			return imageTestJSON(t, []any{map[string]any{"Id": strings.Repeat("a", 64), "Image": "sha256:" + strings.Repeat("b", 64), "Config": config, "HostConfig": host, "State": state, "Mounts": mounts}}), nil
		case args[0] == "start":
			started = true
			if kind == "run-error" {
				return []byte("zitadel version 4.20.0\n"), fmt.Errorf("failed after plausible output")
			}
			if kind == "version" {
				return []byte("zitadel version other\n"), nil
			}
			return []byte("zitadel version 4.20.0\n"), nil
		case args[0] == "cp":
			if strings.Contains(args[1], ":/usr/share/") {
				imagePath := strings.SplitN(args[1], ":", 2)[1]
				var original string
				switch imagePath {
				case "/usr/share/doc/base-files/copyright":
					original = "base-files-copyright.raw"
				case "/usr/share/doc/netbase/copyright":
					original = "netbase-copyright.raw"
				case "/usr/share/doc/tzdata/copyright":
					original = "tzdata-copyright.raw"
				case "/usr/share/doc/tzdata-legacy/copyright":
					original = "tzdata-legacy-copyright.raw"
				case "/usr/share/doc/media-types/copyright":
					original = "media-types-copyright.raw"
				case "/usr/share/doc/ca-certificates/copyright":
					original = "ca-certificates-copyright.raw"
				case "/usr/share/common-licenses/GPL-2":
					original = "GPL-2.raw"
				default:
					t.Fatalf("unexpected base notice path: %s", imagePath)
				}
				b, err := os.ReadFile(filepath.Join("testdata/zitadel-source/base", original))
				if err != nil {
					return nil, err
				}
				return nil, os.WriteFile(args[2], b, 0644)
			}

			context := filepath.Join(filepath.Dir(args[2]), "context")
			err := filepath.WalkDir(context, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				rel, e := filepath.Rel(context, p)
				if e != nil {
					return e
				}
				if d.IsDir() {
					return os.MkdirAll(filepath.Join(args[2], rel), 0700)
				}
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				st, e := os.Stat(p)
				if e != nil {
					return e
				}
				return os.WriteFile(filepath.Join(args[2], rel), b, st.Mode().Perm())
			})
			if kind == "copied-binary" {
				imageTestWrite(t, args[2], "zitadel", []byte("substituted image payload"))
			}
			return nil, err
		case args[0] == "rm" || (args[0] == "image" && args[1] == "rm"):
			if kind == "cleanup" {
				return nil, fmt.Errorf("cleanup failed")
			}
			return nil, nil
		case args[0] == "image" && args[1] == "save":
			if len(args) != 5 || args[2] != "--output" || args[4] != "sha256:"+strings.Repeat("b", 64) {
				t.Fatalf("candidate must preserve the exercised image ID: %s", command)
			}
			return nil, os.WriteFile(args[3], []byte("opaque Docker image fixture\n"), 0600)
		case args[0] == "image" && args[1] == "load":
			return []byte("Loaded image ID: sha256:" + strings.Repeat("b", 64) + "\n"), nil
		case args[0] == "image" && args[1] == "ls":
			if len(args) != 7 || args[2] != "--all" || args[3] != "--no-trunc" || args[4] != "--quiet" || args[5] != "--filter" || args[6] != "label=org.devantler-tech.identity-smoke-owner="+owner {
				t.Fatalf("absence check must cover only this owner's complete image set: %s", command)
			}
			return nil, nil
		default:
			t.Fatalf("unexpected Docker command: %s", command)
		}
		return nil, nil
	}
}

// This exercises the orchestration and opaque handoff only. A process double
// cannot prove Docker archive semantics or real registry acceptance.
func TestImageSmokePreservesTheExercisedCandidateBeforeCleanup(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	base := imageSmokeDouble(t, build, "")
	probeComplete, saved, cleaned := false, false, false
	run := func(d time.Duration, name string, args ...string) ([]byte, error) {
		if name == "docker" && args[0] == "image" && args[1] == "save" {
			if !probeComplete || cleaned {
				t.Fatal("candidate saved before probes or after cleanup")
			}
			saved = true
		}
		if name == "docker" && (args[0] == "rm" || (args[0] == "image" && args[1] == "rm")) {
			cleaned = true
		}
		return base(d, name, args...)
	}
	if err := imageSmoke(build, repo, out, run, func(_, _ string) error { probeComplete = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !saved || !cleaned {
		t.Fatal("successful smoke discarded the tested candidate")
	}
	var receipt struct {
		LocalImageID      string `json:"localImageId"`
		CandidateReloaded bool   `json:"candidateReloaded"`
		Candidate         struct {
			Path   string `json:"path"`
			Bytes  int64  `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"candidateArchive"`
	}
	b, err := os.ReadFile(filepath.Join(out, "image-smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.CandidateReloaded || receipt.LocalImageID != "sha256:"+strings.Repeat("b", 64) || receipt.Candidate.Path != "candidate-image.tar" || receipt.Candidate.Bytes != 28 {
		t.Fatalf("candidate identity/size missing: %+v", receipt)
	}
	archive, err := os.ReadFile(filepath.Join(out, receipt.Candidate.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(archive) != "opaque Docker image fixture\n" || receipt.Candidate.SHA256 != "76f5a0b00771eaed54175678f940fc8cbcb82ac2052ec00d688ae98ef2adae7c" {
		t.Fatal("candidate bytes are not bound to the smoke receipt")
	}
	st, err := os.Stat(filepath.Join(out, receipt.Candidate.Path))
	if err != nil || st.Mode().Perm() != 0444 {
		t.Fatal("candidate was not sealed read-only")
	}
}

func TestImageSmokeCandidateRequiresAFreshLoadAfterOriginalRemoval(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	base := imageSmokeDouble(t, build, "")
	removals, loads := 0, 0
	run := func(d time.Duration, name string, args ...string) ([]byte, error) {
		if name == "docker" && args[0] == "image" && args[1] == "rm" {
			removals++
		}
		if name == "docker" && args[0] == "image" && args[1] == "load" {
			wantArchive, err := filepath.EvalSymlinks(filepath.Join(out, "candidate-image.tar"))
			if err != nil {
				t.Fatal(err)
			}
			if removals != 1 || len(args) != 4 || args[2] != "--input" || args[3] != wantArchive {
				t.Fatalf("candidate load must follow original removal and use only the sealed archive: removals=%d args=%q", removals, args)
			}
			loads++
			return []byte("Loaded image ID: sha256:" + strings.Repeat("b", 64) + "\n"), nil
		}
		return base(d, name, args...)
	}
	if err := imageSmoke(build, repo, out, run); err != nil {
		t.Fatal(err)
	}
	if loads != 1 || removals != 2 {
		t.Fatalf("candidate was not loaded, verified and cleaned: loads=%d removals=%d", loads, removals)
	}
}

func TestImageSmokeRejectsFailedOrChangedCandidateRoundtrip(t *testing.T) {
	for _, kind := range []string{"absence-error", "original-present", "load-error", "wrong-id", "wrong-owner", "wrong-config", "changed-archive"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
			out := filepath.Join(t.TempDir(), "smoke")
			base := imageSmokeDouble(t, build, "")
			loaded := false
			run := func(d time.Duration, name string, args ...string) ([]byte, error) {
				if name == "docker" && args[0] == "image" && args[1] == "ls" {
					if kind == "absence-error" {
						return nil, fmt.Errorf("fixture absence check failed")
					}
					if kind == "original-present" {
						return []byte("sha256:" + strings.Repeat("b", 64) + "\n"), nil
					}
				}
				if name == "docker" && args[0] == "image" && args[1] == "load" {
					loaded = true
					if kind == "load-error" {
						return []byte("plausible loaded output"), fmt.Errorf("fixture load failed")
					}
					if kind == "changed-archive" {
						imageTestWrite(t, out, "candidate-image.tar", []byte("changed after export\n"))
					}
				}
				b, err := base(d, name, args...)
				if loaded && name == "docker" && args[0] == "image" && args[1] == "inspect" && err == nil {
					var images []map[string]any
					if err := json.Unmarshal(b, &images); err != nil {
						t.Fatal(err)
					}
					switch kind {
					case "wrong-id":
						images[0]["Id"] = "sha256:" + strings.Repeat("c", 64)
					case "wrong-owner":
						images[0]["Config"].(map[string]any)["Labels"] = map[string]string{"org.devantler-tech.identity-smoke-owner": "foreign"}
					case "wrong-config":
						images[0]["Config"].(map[string]any)["User"] = "0"
					}
					b = imageTestJSON(t, images)
				}
				return b, err
			}
			if err := imageSmoke(build, repo, out, run); err == nil {
				t.Fatal("invalid candidate roundtrip admitted")
			}
			if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
				t.Fatal("failed roundtrip emitted a successful receipt")
			}
		})
	}
}

func TestImageSmokeNeverLoadsACandidateChangedDuringCleanup(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	base := imageSmokeDouble(t, build, "")
	run := func(d time.Duration, name string, args ...string) ([]byte, error) {
		if name == "docker" && args[0] == "image" && args[1] == "ls" {
			imageTestWrite(t, out, "candidate-image.tar", []byte("substituted before reload\n"))
		}
		if name == "docker" && args[0] == "image" && args[1] == "load" {
			t.Fatal("changed candidate reached Docker load")
		}
		return base(d, name, args...)
	}
	if err := imageSmoke(build, repo, out, run); err == nil {
		t.Fatal("changed candidate accepted")
	}
	if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
		t.Fatal("changed candidate emitted success")
	}
}

func TestImageSmokeRejectsIncompleteCandidateExport(t *testing.T) {
	for _, kind := range []string{"failed", "missing", "empty", "directory", "symlink", "hardlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
			out := filepath.Join(t.TempDir(), "smoke")
			base := imageSmokeDouble(t, build, "")
			run := func(d time.Duration, name string, args ...string) ([]byte, error) {
				if name != "docker" || args[0] != "image" || args[1] != "save" {
					return base(d, name, args...)
				}
				p := args[3]
				switch kind {
				case "failed":
					if err := os.WriteFile(p, []byte("plausible partial archive"), 0600); err != nil {
						t.Fatal(err)
					}
					return []byte("plausible success output"), fmt.Errorf("fixture failed after partial export")
				case "missing":
					return nil, nil
				case "empty":
					return nil, os.WriteFile(p, nil, 0600)
				case "directory":
					return nil, os.Mkdir(p, 0700)
				case "symlink", "hardlink":
					foreign := filepath.Join(t.TempDir(), "foreign")
					if err := os.WriteFile(foreign, []byte("foreign bytes"), 0600); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						return nil, os.Symlink(foreign, p)
					}
					return nil, os.Link(foreign, p)
				case "oversized":
					f, err := os.Create(p)
					if err != nil {
						t.Fatal(err)
					}
					err = f.Truncate((1 << 30) + 1)
					closeErr := f.Close()
					if err != nil {
						return nil, err
					}
					return nil, closeErr
				}
				return nil, fmt.Errorf("unexpected case")
			}
			if err := imageSmoke(build, repo, out, run); err == nil {
				t.Fatal("invalid export emitted success")
			}
			if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
				t.Fatal("failed export retained a success receipt")
			}
		})
	}
}

func TestRestrictedImageSmokeCompletesOnlyAfterObservedSuccess(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	if err := imageSmoke(build, repo, out, imageSmokeDouble(t, build, "")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "image-smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("cli-version-smoke")) || !bytes.Contains(b, []byte("unknown")) {
		t.Fatal("missing honest scope")
	}
	var receipt struct {
		BaseRootFS imageRootFS `json:"baseRootFS"`
	}
	if err := json.Unmarshal(b, &receipt); err != nil || receipt.BaseRootFS.Type != "layers" || strings.Join(receipt.BaseRootFS.Layers, "\n") != strings.Join(imageBaseFixtureLayers(t), "\n") {
		t.Fatal("actual inspected base filesystem is absent from the receipt")
	}
}

func TestServiceProbeFailureCannotEmitSuccessfulImageReceipt(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	called := 0
	err := imageSmoke(build, repo, out, imageSmokeDouble(t, build, ""), func(id, owner string) error {
		called++
		if id != "sha256:"+strings.Repeat("b", 64) || len(owner) != 32 {
			t.Fatal("service probe received unbound identity")
		}
		return fmt.Errorf("fixture service refused")
	})
	if err == nil || called != 1 || !strings.Contains(err.Error(), "fixture service refused") {
		t.Fatalf("failed service was ignored: calls=%d err=%v", called, err)
	}
	if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
		t.Fatal("failed service emitted successful image receipt")
	}
}

func TestRestrictedImageSmokeRejectsFailedOrWeakenedExecution(t *testing.T) {
	for _, kind := range []string{"verification-error", "copied-verification-error", "seccomp", "build-error", "image-user", "architecture", "container-user", "writable", "privileged", "capability", "network", "no-limits", "mount", "oom", "exit", "run-error", "version", "copied-binary", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
			out := filepath.Join(t.TempDir(), "smoke")
			if imageSmoke(build, repo, out, imageSmokeDouble(t, build, kind)) == nil {
				t.Fatal("invalid execution admitted")
			}
			if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
				t.Fatal("failed smoke emitted a success receipt")
			}
		})
	}
}

func TestImageCleanupNeverDeletesAnUnownedObject(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	base := imageSmokeDouble(t, build, "")
	deleted := false
	run := func(d time.Duration, name string, args ...string) ([]byte, error) {
		if name == "docker" && (args[0] == "rm" || (args[0] == "image" && args[1] == "rm")) {
			deleted = true
		}
		b, err := base(d, name, args...)
		if name == "docker" && args[0] == "image" && args[1] == "inspect" && err == nil {
			var images []map[string]any
			if e := json.Unmarshal(b, &images); e != nil {
				t.Fatal(e)
			}
			images[0]["Config"].(map[string]any)["Labels"] = map[string]string{"org.devantler-tech.identity-smoke-owner": "foreign"}
			b = imageTestJSON(t, images)
		}
		return b, err
	}
	if imageSmoke(build, repo, out, run) == nil {
		t.Fatal("unowned image accepted")
	}
	if deleted {
		t.Fatal("cleanup attempted to delete an unowned object")
	}
}

// These controls exercise the actual Docker-copy boundary, not a notice summary.
func TestImageSmokeRejectsMissingOrSubstitutedBaseCopyrights(t *testing.T) {
	for _, pkg := range []string{"base-files", "netbase", "tzdata", "tzdata-legacy", "media-types", "ca-certificates", "GPL-2"} {
		for _, kind := range []string{"missing", "substituted"} {
			t.Run(pkg+"/"+kind, func(t *testing.T) {
				build, repo, _ := imageTestFixture(t)
				imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
				base := imageSmokeDouble(t, build, "")
				imagePath := "/usr/share/doc/" + pkg + "/copyright"
				if pkg == "GPL-2" {
					imagePath = "/usr/share/common-licenses/GPL-2"
				}
				run := func(d time.Duration, name string, args ...string) ([]byte, error) {
					if name == "docker" && len(args) == 3 && args[0] == "cp" && strings.HasSuffix(args[1], ":"+imagePath) {
						if kind == "missing" {
							return nil, fmt.Errorf("original copyright missing")
						}
						return nil, os.WriteFile(args[2], []byte("substituted original copyright\n"), 0644)
					}
					return base(d, name, args...)
				}
				out := filepath.Join(t.TempDir(), "smoke")
				if err := imageSmoke(build, repo, out, run); err == nil {
					t.Fatal("missing or substituted base copyright accepted")
				}
				if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
					t.Fatal("invalid base produced success receipt")
				}
			})
		}
	}
}

func TestImageSmokeRetainsOriginalBaseNoticeCopies(t *testing.T) {
	build, repo, _ := imageTestFixture(t)
	imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
	out := filepath.Join(t.TempDir(), "smoke")
	if err := imageSmoke(build, repo, out, imageSmokeDouble(t, build, "")); err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		BaseNotices []imageBaseNotice `json:"baseNotices"`
	}
	raw, err := os.ReadFile(filepath.Join(out, "image-smoke.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	originals := []string{"base-files", "netbase", "tzdata", "tzdata-legacy", "media-types", "ca-certificates", "GPL-2"}
	if len(receipt.BaseNotices) != len(originals) {
		t.Fatal("incomplete original copyright receipt")
	}
	for i, name := range originals {
		fixture := name + "-copyright.raw"
		wantPath := "/usr/share/doc/" + name + "/copyright"
		if name == "GPL-2" {
			fixture = "GPL-2.raw"
			wantPath = "/usr/share/common-licenses/GPL-2"
		}
		want, err := os.ReadFile(filepath.Join("testdata/zitadel-source/base", fixture))
		if err != nil {
			t.Fatal(err)
		}
		notice := receipt.BaseNotices[i]
		got, err := os.ReadFile(filepath.Join(out, notice.File.Path))
		if err != nil {
			t.Fatal(err)
		}
		if notice.ImagePath != wantPath || !bytes.Equal(got, want) || notice.File.SHA256 != imageHash(want) || notice.File.Bytes != len(want) || notice.File.Mode != 0644 {
			t.Fatal("original base copyright bytes/receipt differ")
		}
	}
}
func TestImageSmokeRejectsLateBaseNoticeReplacement(t *testing.T) {
	for _, when := range []string{"probe", "reload"} {
		t.Run(when, func(t *testing.T) {
			build, repo, _ := imageTestFixture(t)
			imageTestWrite(t, repo, "Dockerfile.zitadel", []byte("fixture Dockerfile\n"))
			out := filepath.Join(t.TempDir(), "smoke")
			base := imageSmokeDouble(t, build, "")
			replace := func() {
				if err := os.WriteFile(filepath.Join(out, "base-notices/GPL-2.txt"), []byte("replaced late\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			run := func(d time.Duration, name string, args ...string) ([]byte, error) {
				b, err := base(d, name, args...)
				if when == "reload" && name == "docker" && args[0] == "image" && args[1] == "load" {
					replace()
				}
				return b, err
			}
			probe := func(string, string) error {
				if when == "probe" {
					replace()
				}
				return nil
			}
			if err := imageSmoke(build, repo, out, run, probe); err == nil {
				t.Fatal("late base copyright substitution admitted")
			}
			if _, err := os.Stat(filepath.Join(out, "image-smoke.json")); !os.IsNotExist(err) {
				t.Fatal("late substitution emitted success")
			}
		})
	}
}
