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
			return imageTestJSON(t, []any{map[string]any{"Id": "sha256:" + strings.Repeat("b", 64), "Os": "linux", "Architecture": arch, "Config": map[string]any{"User": user, "Entrypoint": []string{"/app/zitadel"}, "Cmd": []string{"--version"}, "WorkingDir": "/app", "Labels": map[string]string{"org.devantler-tech.identity-smoke-owner": owner}}}}), nil
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
		default:
			t.Fatalf("unexpected Docker command: %s", command)
		}
		return nil, nil
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
	for _, kind := range []string{"verification-error", "seccomp", "build-error", "image-user", "architecture", "container-user", "writable", "privileged", "capability", "network", "no-limits", "mount", "oom", "exit", "run-error", "version", "copied-binary", "cleanup"} {
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
