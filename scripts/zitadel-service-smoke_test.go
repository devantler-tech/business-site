package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestServiceHTTPDiagnosticDoesNotExposeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = io.WriteString(w, "fictional private response must not appear in diagnostic")
	}))
	defer server.Close()
	_, err := serviceGet(server.Client(), server.URL, "/debug/ready")
	if err == nil || !strings.Contains(err.Error(), "status=412") || strings.Contains(err.Error(), "fictional") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("response diagnostic was missing status or exposed body/URL: %v", err)
	}
	server.Close()
	_, err = serviceGet(server.Client(), server.URL, "/debug/ready")
	if err == nil || !strings.Contains(err.Error(), "connectionRefused=true") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("transport diagnostic was missing category or exposed URL: %v", err)
	}
}

func TestServiceTargetUsesOnlyOwnedPrivateEndpoint(t *testing.T) {
	check := func(raw string) (string, error) {
		var container imageContainer
		if err := json.Unmarshal([]byte(raw), &container); err != nil {
			t.Fatal(err)
		}
		return serviceTarget(container, "owned-network")
	}
	const good = `{"NetworkSettings":{"Networks":{"fixture":{"NetworkID":"owned-network","IPAddress":"172.18.0.2"}}}}`
	if target, err := check(good); err != nil || target != "172.18.0.2:8080" {
		t.Fatalf("owned internal address refused: %q %v", target, err)
	}
	for _, raw := range []string{
		`{}`, strings.Replace(good, "owned-network", "foreign-network", 1),
		strings.Replace(good, "172.18.0.2", "", 1),
		strings.Replace(good, "172.18.0.2", "127.0.0.1", 1),
		strings.Replace(good, "172.18.0.2", "169.254.169.254", 1),
		strings.Replace(good, "172.18.0.2", "8.8.8.8", 1),
		strings.Replace(good, "172.18.0.2", "172.18.0.2:80", 1),
		strings.Replace(good, "172.18.0.2", "::1", 1),
		strings.Replace(good, "172.18.0.2", "service.example.invalid", 1),
		`{"NetworkSettings":{"Networks":{"a":{"NetworkID":"owned-network","IPAddress":"172.18.0.2"},"b":{"NetworkID":"other","IPAddress":"172.19.0.2"}}}}`,
	} {
		if target, err := check(raw); err == nil || target != "" {
			t.Fatalf("unsafe fixture endpoint admitted: %q", target)
		}
	}
}

// TestServiceTransportRefusesUnrelatedAuthority rejects alternate authorities,
// ports and raw endpoints before the fixture transport can reach them.
func TestServiceTransportRefusesUnrelatedAuthority(t *testing.T) {
	transport := serviceTransport("172.18.0.2:8080")
	defer transport.CloseIdleConnections()
	for _, address := range []string{"elsewhere.invalid:8080", "fixture.localhost:80", "127.0.0.1:8080", "169.254.169.254:80"} {
		if connection, err := transport.DialContext(context.Background(), "tcp", address); err == nil || connection != nil {
			t.Fatalf("unrelated authority admitted: %q", address)
		}
	}
	if connection, err := transport.DialContext(context.Background(), "udp", serviceAuthority); err == nil || connection != nil {
		t.Fatal("non-HTTP transport admitted")
	}
}

func TestServiceTransportKeepsAuthorityWithoutDNS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != serviceAuthority || r.URL.Path != "/debug/ready" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "ready")
	}))
	defer server.Close()
	// The loopback endpoint belongs only to this unit test; the runtime obtains
	// its private endpoint through the separate owned-network admission above.
	transport := serviceTransport(strings.TrimPrefix(server.URL, "http://"))
	defer transport.CloseIdleConnections()
	b, err := serviceGet(&http.Client{Transport: transport}, "http://"+serviceAuthority, "/debug/ready")
	if err != nil || string(b) != "ready" {
		t.Fatalf("fixed connection lost the fictional HTTP authority: %q %v", b, err)
	}
}

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

// TestExplicitIdentityBrowserProfile verifies that the opt-in browser request
// reaches native-platform admission rather than an unknown-argument shortcut.
func TestExplicitIdentityBrowserProfile(t *testing.T) {
	var out, diagnostic bytes.Buffer
	status := imageRun([]string{"--browser-smoke", "--build", t.TempDir(), "--repository", t.TempDir(), "--output", t.TempDir()}, &out, &diagnostic)
	native := runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && runtime.Version() == "go1.26.10"
	if status == 0 || strings.Contains(diagnostic.String(), "unexpected argument") || (!native && !strings.Contains(diagnostic.String(), "native Linux amd64 Go 1.26.10 required")) {
		t.Fatalf("browser request did not reach native admission: status=%d diagnostic=%q", status, diagnostic.String())
	}
}

// A running service has a separate closed profile. These literal Docker fields
// must not weaken the existing network-none, no-mount version-command profile.
func TestServiceContainerAdmission(t *testing.T) {
	const raw = `[{"Id":"service-id","Image":"sha256:image","Config":{"Image":"sha256:image","User":"65532:65532","Entrypoint":["/app/zitadel"],"Cmd":["start-from-init","--masterkeyFromEnv","--tlsMode","disabled"],"WorkingDir":"/app","Labels":{"org.devantler-tech.identity-smoke-owner":"fixture-owner"}},"HostConfig":{"ReadonlyRootfs":true,"Privileged":false,"CapDrop":["ALL"],"SecurityOpt":["no-new-privileges"],"NetworkMode":"network-id","IpcMode":"private","PidsLimit":128,"Memory":1073741824,"NanoCpus":2000000000,"Tmpfs":{"/tmp":"rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700"}},"State":{"Status":"running","Running":true,"Pid":4321},"Mounts":[{"Type":"tmpfs","Destination":"/tmp","RW":true}],"NetworkSettings":{"Networks":{"fixture":{"NetworkID":"network-id"}}}}]`
	check := func(b []byte) error {
		_, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", "running")
		return err
	}
	if err := check([]byte(raw)); err != nil {
		t.Fatalf("valid service profile refused: %v", err)
	}
	// Legacy --tmpfs uses HostConfig.Tmpfs rather than MountPoints. The live
	// kernel mount must independently pass before service success is possible.
	var legacy []map[string]any
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy[0]["Mounts"] = nil
	legacyBytes, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(legacyBytes); err != nil {
		t.Fatalf("legacy Docker tmpfs metadata refused: %v", err)
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
		if _, err := serviceInspect(b, "service-id", "sha256:image", "fixture-owner", "network-id", status); err != nil {
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
		"publish all":   func(c map[string]any) { c["HostConfig"].(map[string]any)["PublishAllPorts"] = true },
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
		"missing declared tmpfs": func(c map[string]any) { c["HostConfig"].(map[string]any)["Tmpfs"] = nil },
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

const serviceMountFixture = `100 99 0:101 / / ro,relatime - overlay overlay rw,lowerdir=/fictional/image
101 100 0:102 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
102 100 0:103 / /tmp rw,nosuid,nodev,noexec,relatime - tmpfs tmpfs rw,size=16384k,mode=700,uid=65532,gid=65532
`

func TestActualKernelRootAndApplicationTmpfs(t *testing.T) {
	m, err := serviceMountStatus([]byte(serviceMountFixture))
	if err != nil || !m.RootReadOnly || m.TmpfsBytes != 16777216 || m.UID != 65532 || m.GID != 65532 || m.Mode != "0700" || !m.NoExec || !m.NoSuid || !m.NoDev {
		t.Fatalf("valid kernel mount proof refused: %#v %v", m, err)
	}
	for name, change := range map[string][2]string{
		"writable root":         {"/ / ro,relatime", "/ / rw,relatime"},
		"missing tmpfs":         {"102 100 0:103 / /tmp", "102 100 0:103 / /elsewhere"},
		"bind instead of tmpfs": {"- tmpfs tmpfs", "- ext4 /dev/fixture"},
		"executable":            {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nosuid,nodev,relatime - tmpfs"},
		"suid":                  {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nodev,noexec,relatime - tmpfs"},
		"devices":               {"rw,nosuid,nodev,noexec,relatime - tmpfs", "rw,nosuid,noexec,relatime - tmpfs"},
		"wrong limit":           {"size=16384k", "size=32m"},
		"root-owned tmpfs":      {"uid=65532", "uid=0"},
		"wrong group":           {"gid=65532", "gid=0"},
		"world writable":        {"mode=700", "mode=777"},
		"missing uid":           {"uid=65532,", ""},
		"duplicate option":      {"uid=65532", "uid=65532,uid=0"},
	} {
		t.Run(name, func(t *testing.T) {
			b := strings.Replace(serviceMountFixture, change[0], change[1], 1)
			if b == serviceMountFixture {
				t.Fatal("unchanged mutation")
			}
			if _, err := serviceMountStatus([]byte(b)); err == nil {
				t.Fatal("unsafe kernel mount accepted")
			}
		})
	}
	for _, extra := range []string{"malformed\n", "103 100 0:104 / /tmp rw - tmpfs tmpfs rw\n", "103 102 0:104 / /tmp/hidden rw - ext4 /dev/fixture rw\n"} {
		if _, err := serviceMountStatus([]byte(serviceMountFixture + extra)); err == nil {
			t.Fatal("ambiguous or masked kernel mount accepted")
		}
	}
}

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
