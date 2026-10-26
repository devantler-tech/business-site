package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// A service request must reach native-platform admission, rather than being
// mistaken for the existing version-only profile. Actual service acceptance is
// a separate mandatory native Linux CI invocation.
func TestExplicitIdentityServiceProfile(t *testing.T) {
	var out, diagnostic bytes.Buffer
	status := imageRun([]string{"--service-smoke", "--build", t.TempDir(), "--repository", t.TempDir(), "--output", t.TempDir()}, &out, &diagnostic)
	native := runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && runtime.Version() == "go1.26.10"
	if status == 0 || strings.Contains(diagnostic.String(), "unexpected argument") || (!native && !strings.Contains(diagnostic.String(), "native Linux amd64 Go 1.26.10 required")) {
		t.Fatalf("service request did not reach native admission: status=%d diagnostic=%q", status, diagnostic.String())
	}
}

// A running service has a separate closed profile. These literal Docker fields
// must not weaken the existing network-none, no-mount version-command profile.
func TestServiceContainerAdmission(t *testing.T) {
	const raw = `[{"Id":"service-id","Image":"sha256:image","Config":{"Image":"sha256:image","User":"65532:65532","Entrypoint":["/app/zitadel"],"Cmd":["start-from-init","--masterkeyFromEnv","--tlsMode","disabled"],"WorkingDir":"/app","Labels":{"org.devantler-tech.identity-smoke-owner":"fixture-owner"}},"HostConfig":{"ReadonlyRootfs":true,"Privileged":false,"CapDrop":["ALL"],"SecurityOpt":["no-new-privileges"],"NetworkMode":"network-id","IpcMode":"private","PidsLimit":128,"Memory":1073741824,"NanoCpus":2000000000,"Tmpfs":{"/tmp":"rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700"},"PortBindings":{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"12345"}]}},"State":{"Status":"running","Running":true,"Pid":4321},"Mounts":[{"Type":"tmpfs","Destination":"/tmp","RW":true}],"NetworkSettings":{"Networks":{"fixture":{"NetworkID":"network-id"}}}}]`
	check := func(b []byte) error {
		_, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", "12345", "running")
		return err
	}
	if err := check([]byte(raw)); err != nil {
		t.Fatalf("valid service profile refused: %v", err)
	}
	for _, status := range []string{"created", "exited"} {
		var all []map[string]any
		if err := json.Unmarshal([]byte(raw), &all); err != nil {
			t.Fatal(err)
		}
		all[0]["State"] = map[string]any{"Status": status, "Running": false, "Pid": 0}
		all[0]["Mounts"] = nil
		all[0]["NetworkSettings"] = map[string]any{}
		b, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", "12345", status); err != nil {
			t.Fatalf("inactive container incorrectly required active kernel resources: %v", err)
		}
	}
	for name, mutation := range map[string]func(map[string]any){
		"wrong owner":   func(c map[string]any) { c["Config"].(map[string]any)["Labels"] = map[string]any{} },
		"wrong image":   func(c map[string]any) { c["Image"] = "sha256:other" },
		"root":          func(c map[string]any) { c["Config"].(map[string]any)["User"] = "0" },
		"version only":  func(c map[string]any) { c["Config"].(map[string]any)["Cmd"] = []string{"--version"} },
		"writable root": func(c map[string]any) { c["HostConfig"].(map[string]any)["ReadonlyRootfs"] = false },
		"privileged":    func(c map[string]any) { c["HostConfig"].(map[string]any)["Privileged"] = true },
		"unconfined": func(c map[string]any) {
			c["HostConfig"].(map[string]any)["SecurityOpt"] = []string{"no-new-privileges", "seccomp=unconfined"}
		},
		"extra tmpfs":  func(c map[string]any) { c["HostConfig"].(map[string]any)["Tmpfs"].(map[string]any)["/app"] = "rw" },
		"host network": func(c map[string]any) { c["HostConfig"].(map[string]any)["NetworkMode"] = "host" },
		"public bind": func(c map[string]any) {
			c["HostConfig"].(map[string]any)["PortBindings"] = map[string]any{"8080/tcp": []any{map[string]any{"HostIp": "0.0.0.0", "HostPort": "12345"}}}
		},
		"extra mount": func(c map[string]any) {
			c["Mounts"] = append(c["Mounts"].([]any), map[string]any{"Type": "bind", "Destination": "/etc", "RW": false})
		},
		"missing active mount":   func(c map[string]any) { c["Mounts"] = nil },
		"missing active network": func(c map[string]any) { c["NetworkSettings"] = map[string]any{} },
		"oom":                    func(c map[string]any) { c["State"].(map[string]any)["OOMKilled"] = true },
		"extra network": func(c map[string]any) {
			c["NetworkSettings"].(map[string]any)["Networks"].(map[string]any)["foreign"] = map[string]any{"NetworkID": "foreign"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var all []map[string]any
			if err := json.Unmarshal([]byte(raw), &all); err != nil {
				t.Fatal(err)
			}
			mutation(all[0])
			b, err := json.Marshal(all)
			if err != nil {
				t.Fatal(err)
			}
			if check(b) == nil {
				t.Fatal("unsafe service profile accepted")
			}
		})
	}
	if _, err := imageInspectContainer([]byte(raw), "service-id", "sha256:image", "running"); err == nil {
		t.Fatal("service profile weakened CLI-only admission")
	}
}

const serviceStatusFixture = `Name:	zitadel
Pid:	12345
NSpid:	12345	1
Uid:	65532	65532	65532	65532
Gid:	65532	65532	65532	65532
VmRSS:	47104 kB
VmHWM:	65536 kB
Threads:	12
CapInh:	0000000000000000
CapPrm:	0000000000000000
CapEff:	0000000000000000
CapBnd:	0000000000000000
CapAmb:	0000000000000000
NoNewPrivs:	1
Seccomp:	2
`

func TestActualProcessRestrictionsAndMeasuredMemory(t *testing.T) {
	p, err := serviceProcessStatus([]byte(serviceStatusFixture), 12345)
	if err != nil || p.PID != 12345 || p.Name != "zitadel" || p.UID != 65532 || p.GID != 65532 || p.RSSKiB != 47104 || p.PeakRSSKiB != 65536 || p.Threads != 12 || p.EffectiveCapabilities != "0000000000000000" || p.BoundingCapabilities != "0000000000000000" || !p.NoNewPrivileges || p.SeccompMode != 2 {
		t.Fatalf("valid measured process refused: %#v %v", p, err)
	}
	for name, replacement := range map[string][2]string{
		"root real uid":        {"65532\t65532\t65532\t65532", "0\t65532\t65532\t65532"},
		"effective root gid":   {"Gid:\t65532\t65532\t65532\t65532", "Gid:\t65532\t0\t65532\t65532"},
		"capability":           {"CapEff:\t0000000000000000", "CapEff:\t0000000000000001"},
		"bounding capability":  {"CapBnd:\t0000000000000000", "CapBnd:\t0000000000000001"},
		"privilege escalation": {"NoNewPrivs:\t1", "NoNewPrivs:\t0"},
		"no seccomp":           {"Seccomp:\t2", "Seccomp:\t0"},
		"not namespace init":   {"12345\t1", "12345\t2"},
		"wrong host pid":       {"Pid:\t12345", "Pid:\t12346"},
		"no resident memory":   {"VmRSS:\t47104 kB", "VmRSS:\t0 kB"},
		"missing peak":         {"VmHWM:\t65536 kB\n", ""},
		"wrong units":          {"47104 kB", "47104 MB"},
		"peak below rss":       {"65536 kB", "1024 kB"},
		"duplicate evidence":   {"Seccomp:\t2", "Seccomp:\t2\nSeccomp:\t0"},
	} {
		t.Run(name, func(t *testing.T) {
			changed := strings.Replace(serviceStatusFixture, replacement[0], replacement[1], 1)
			if changed == serviceStatusFixture {
				t.Fatal("negative fixture did not change")
			}
			if _, err := serviceProcessStatus([]byte(changed), 12345); err == nil {
				t.Fatal("unsafe or unmeasured process accepted")
			}
		})
	}
}
