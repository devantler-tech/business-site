package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const serviceAuthority = "fixture.invalid:8080"

// Docker does not publish ports for an internal-only network. Native Linux
// can reach its bridge directly; bind that connection to the inspected owned
// container without resolving the fictional HTTP authority through DNS.
func serviceTarget(c imageContainer, network string) (string, error) {
	if network != "" && len(c.NetworkSettings.Networks) == 1 {
		for _, endpoint := range c.NetworkSettings.Networks {
			ip := net.ParseIP(endpoint.IPAddress)
			if endpoint.NetworkID == network && ip != nil && ip.To4() != nil && ip.IsPrivate() && ip.String() == endpoint.IPAddress {
				return net.JoinHostPort(endpoint.IPAddress, "8080"), nil
			}
		}
	}
	return "", fmt.Errorf("owned internal service endpoint unavailable")
}

func serviceTransport(target string) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != serviceAuthority {
			return nil, fmt.Errorf("fixture connection escaped")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", target)
	}}
}

// Keep failure diagnostics structural: never return response bodies, URLs,
// container output or configuration from this fictional fixture.
func serviceGet(client *http.Client, base, path string) ([]byte, error) {
	response, err := client.Get(base + path)
	if err != nil {
		var networkError net.Error
		timedOut := errors.As(err, &networkError) && networkError.Timeout()
		return nil, fmt.Errorf("fixture HTTP transport failed (timeout=%t connectionRefused=%t connectionReset=%t)", timedOut, errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ECONNRESET))
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || len(b) == 2<<20 || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fixture HTTP response invalid (status=%d bodyReadFailed=%t oversized=%t)", response.StatusCode, err != nil, len(b) == 2<<20)
	}
	return b, nil
}

type serviceProcess struct {
	PID                   int           `json:"pid"`
	Name                  string        `json:"name"`
	UID                   int           `json:"uid"`
	GID                   int           `json:"gid"`
	RSSKiB                int64         `json:"rssKiB"`
	PeakRSSKiB            int64         `json:"peakRssKiB"`
	Threads               int64         `json:"threads"`
	EffectiveCapabilities string        `json:"effectiveCapabilities"`
	BoundingCapabilities  string        `json:"boundingCapabilities"`
	NoNewPrivileges       bool          `json:"noNewPrivileges"`
	SeccompMode           int           `json:"seccompMode"`
	Mounts                serviceMounts `json:"mounts"`
}

type serviceMounts struct {
	RootReadOnly bool   `json:"rootReadOnly"`
	TmpfsBytes   int64  `json:"tmpfsBytes"`
	UID          int    `json:"uid"`
	GID          int    `json:"gid"`
	Mode         string `json:"mode"`
	NoExec       bool   `json:"noExec"`
	NoSuid       bool   `json:"noSuid"`
	NoDev        bool   `json:"noDev"`
}

func serviceMountStatus(b []byte) (serviceMounts, error) {
	bad := func() (serviceMounts, error) {
		return serviceMounts{}, fmt.Errorf("actual kernel root or application tmpfs differs")
	}
	if len(b) == 0 || len(b) > 1<<20 {
		return bad()
	}
	options := func(raw string) (map[string]string, bool) {
		out := map[string]string{}
		for _, option := range strings.Split(raw, ",") {
			key, value, _ := strings.Cut(option, "=")
			if key == "" {
				return nil, false
			}
			if _, exists := out[key]; exists {
				return nil, false
			}
			out[key] = value
		}
		return out, true
	}
	has := func(m map[string]string, key string) bool { _, ok := m[key]; return ok }
	root, tmp := false, false
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		f := strings.Fields(line)
		sep := -1
		for i, field := range f {
			if field == "-" {
				sep = i
				break
			}
		}
		if len(f) < 10 || sep < 6 || len(f) != sep+4 {
			return bad()
		}
		if strings.HasPrefix(f[4], "/tmp/") {
			return bad()
		}
		if f[4] != "/" && f[4] != "/tmp" {
			continue
		}
		mount, ok := options(f[5])
		if !ok {
			return bad()
		}
		if f[4] == "/" {
			if root || !has(mount, "ro") || has(mount, "rw") {
				return bad()
			}
			root = true
			continue
		}
		if tmp || f[3] != "/" || f[sep+1] != "tmpfs" || f[sep+2] != "tmpfs" {
			return bad()
		}
		for _, required := range []string{"rw", "nosuid", "nodev", "noexec"} {
			if !has(mount, required) {
				return bad()
			}
		}
		for _, unsafe := range []string{"ro", "suid", "dev", "exec"} {
			if has(mount, unsafe) {
				return bad()
			}
		}
		super, ok := options(f[sep+3])
		if !ok || !has(super, "rw") || super["uid"] != "65532" || super["gid"] != "65532" {
			return bad()
		}
		mode, e := strconv.ParseUint(super["mode"], 8, 32)
		if e != nil || mode != 0700 {
			return bad()
		}
		size := super["size"]
		multiplier := int64(1)
		if len(size) > 0 {
			switch size[len(size)-1] {
			case 'k':
				multiplier = 1024
				size = size[:len(size)-1]
			case 'm':
				multiplier = 1024 * 1024
				size = size[:len(size)-1]
			case 'g':
				multiplier = 1024 * 1024 * 1024
				size = size[:len(size)-1]
			}
		}
		n, e := strconv.ParseInt(size, 10, 64)
		if e != nil || n <= 0 || n > 16777216 || n*multiplier != 16777216 {
			return bad()
		}
		tmp = true
	}
	if !root || !tmp {
		return bad()
	}
	return serviceMounts{RootReadOnly: true, TmpfsBytes: 16777216, UID: 65532, GID: 65532, Mode: "0700", NoExec: true, NoSuid: true, NoDev: true}, nil
}

func serviceProcessStatus(b []byte, pid int) (serviceProcess, error) {
	bad := func() (serviceProcess, error) {
		return serviceProcess{}, fmt.Errorf("actual service process restrictions or measurements differ")
	}
	fields := map[string][]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if _, exists := fields[key]; exists {
			return bad()
		}
		fields[key] = strings.Fields(value)
	}
	for key, expected := range map[string]string{"Name": "zitadel", "Pid": strconv.Itoa(pid), "Uid": "65532 65532 65532 65532", "Gid": "65532 65532 65532 65532", "NoNewPrivs": "1", "Seccomp": "2", "CapInh": "0000000000000000", "CapPrm": "0000000000000000", "CapEff": "0000000000000000", "CapBnd": "0000000000000000", "CapAmb": "0000000000000000"} {
		if strings.Join(fields[key], " ") != expected {
			return bad()
		}
	}
	ns := fields["NSpid"]
	if pid <= 1 || len(ns) != 2 || ns[0] != strconv.Itoa(pid) || ns[1] != "1" {
		return bad()
	}
	measure := func(key string, memory bool) (int64, bool) {
		f := fields[key]
		if (memory && (len(f) != 2 || f[1] != "kB")) || (!memory && len(f) != 1) {
			return 0, false
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		return n, err == nil && n > 0 && n < 1<<40
	}
	rss, a := measure("VmRSS", true)
	peak, c := measure("VmHWM", true)
	threads, d := measure("Threads", false)
	if !a || !c || !d || peak < rss {
		return bad()
	}
	return serviceProcess{PID: pid, Name: "zitadel", UID: 65532, GID: 65532, RSSKiB: rss, PeakRSSKiB: peak, Threads: threads, EffectiveCapabilities: fields["CapEff"][0], BoundingCapabilities: fields["CapBnd"][0], NoNewPrivileges: true, SeccompMode: 2}, nil
}

const serviceTmpfs = "rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700"
const serviceDatabaseImage = "postgres:17.11-bookworm@sha256:3645570cccdfa447589da9f57dd740faa29b30938e861289a5574b6ca6b03826"

var serviceArgs = []string{"start-from-init", "--masterkeyFromEnv", "--tlsMode", "disabled"}

func serviceInspect(raw []byte, id, image, owner, network, status string) (imageContainer, error) {
	var all []imageContainer
	bad := func() (imageContainer, error) {
		state, exit, mounts := "unknown", 0, 0
		if len(all) == 1 {
			c := all[0]
			switch c.State.Status {
			case "created", "running", "exited", "dead", "paused", "restarting":
				state = c.State.Status
			}
			exit = c.State.ExitCode
			mounts = len(c.Mounts)
		}
		return imageContainer{}, fmt.Errorf("service container restrictions or state differ (state=%s exit=%d mountEntries=%d)", state, exit, mounts)
	}
	if err := imageDecode(raw, &all); err != nil || len(all) != 1 {
		return bad()
	}
	c := all[0]
	h := c.HostConfig
	if c.ID != id || c.Image != image || c.Config.Image != image || c.Config.Labels["org.devantler-tech.identity-smoke-owner"] != owner || c.Config.User != "65532:65532" || c.Config.WorkingDir != "/app" || !reflect.DeepEqual(c.Config.Entrypoint, []string{"/app/zitadel"}) || !reflect.DeepEqual(c.Config.Cmd, serviceArgs) || !h.ReadonlyRootfs || h.Privileged || h.PublishAllPorts || !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || len(h.CapAdd) != 0 || !reflect.DeepEqual(h.SecurityOpt, []string{"no-new-privileges"}) || h.NetworkMode != network || h.PidMode != "" || h.IpcMode != "private" || h.PidsLimit != 128 || h.Memory != 1073741824 || h.NanoCpus != 2000000000 || len(h.Binds) != 0 || len(h.Devices) != 0 || len(h.Tmpfs) != 1 || h.Tmpfs["/tmp"] != serviceTmpfs || len(h.PortBindings) != 0 || len(c.Mounts) > 1 || len(c.NetworkSettings.Networks) > 1 || c.State.Status != status || c.State.Dead || c.State.OOMKilled || c.State.ExitCode != 0 {
		return bad()
	}
	var mount struct {
		Type, Destination string
		RW                bool
	}
	if len(c.Mounts) == 1 {
		if err := json.Unmarshal(c.Mounts[0], &mount); err != nil || mount.Type != "tmpfs" || mount.Destination != "/tmp" || !mount.RW {
			return bad()
		}
	}
	for _, n := range c.NetworkSettings.Networks {
		if n.NetworkID != network && !(status != "running" && n.NetworkID == "") {
			return bad()
		}
	}
	if (status != "running" && status != "created" && status != "exited") || (status == "running" && (!c.State.Running || c.State.Pid <= 1 || len(c.NetworkSettings.Networks) != 1)) || (status == "created" && (c.State.Running || c.State.Pid != 0)) || (status == "exited" && (c.State.Running || c.State.Pid != 0)) {
		return bad()
	}
	return c, nil
}

// This is an isolated HTTP fixture, not the production TLS/authentication
// profile. All database/master/bootstrap values are fictional and short-lived.
func imageService(image, owner, repo, output string, run imageCommand) (result error) {
	source, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil {
		return err
	}
	label := "org.devantler-tech.identity-smoke-owner"
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(owner) {
		return fmt.Errorf("invalid service owner")
	}
	ids := []string{}
	network := ""
	cleaned := false
	cleanup := func() error {
		failed := false
		for i := len(ids) - 1; i >= 0; i-- {
			if _, e := run(time.Minute, "docker", "rm", "--force", ids[i]); e != nil {
				failed = true
			}
		}
		if network != "" {
			if _, e := run(30*time.Second, "docker", "network", "rm", network); e != nil {
				failed = true
			}
		}
		if failed {
			return fmt.Errorf("owned service fixture cleanup failed")
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
	if _, err = run(3*time.Minute, "docker", "pull", "--platform=linux/amd64", serviceDatabaseImage); err != nil {
		return fmt.Errorf("pinned fixture database unavailable")
	}
	b, err := run(30*time.Second, "docker", "network", "create", "--internal", "--label", label+"="+owner, "identity-service-"+owner)
	if err != nil {
		return err
	}
	network = strings.TrimSpace(string(b))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(network) {
		network = ""
		return fmt.Errorf("invalid fixture network identity")
	}
	b, err = run(30*time.Second, "docker", "network", "inspect", network)
	if err != nil {
		return err
	}
	var networks []struct {
		ID         string
		Internal   bool
		Labels     map[string]string
		Containers map[string]json.RawMessage
	}
	if err = imageDecode(b, &networks); err != nil || len(networks) != 1 || networks[0].ID != network || !networks[0].Internal || networks[0].Labels[label] != owner || len(networks[0].Containers) != 0 {
		network = ""
		return fmt.Errorf("fixture network ownership or isolation not confirmed")
	}
	create := func(args ...string) (string, error) {
		b, e := run(30*time.Second, "docker", append([]string{"create", "--label", label + "=" + owner}, args...)...)
		if e != nil {
			return "", fmt.Errorf("fixture container create failed")
		}
		id := strings.TrimSpace(string(b))
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
			return "", fmt.Errorf("invalid fixture container identity")
		}
		b, e = run(30*time.Second, "docker", "inspect", id)
		if e != nil {
			return "", e
		}
		var cs []imageContainer
		if e = imageDecode(b, &cs); e != nil || len(cs) != 1 || cs[0].ID != id || cs[0].Config.Labels[label] != owner {
			return "", fmt.Errorf("fixture container ownership not confirmed")
		}
		ids = append(ids, id)
		return id, nil
	}
	db, err := create("--pull=never", "--platform=linux/amd64", "--network="+network, "--network-alias=database", "--user=999:999", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=512m", "--cpus=2", "--tmpfs=/var/lib/postgresql/data:rw,nosuid,nodev,size=268435456,uid=999,gid=999,mode=0700", "--tmpfs=/var/run/postgresql:rw,nosuid,nodev,size=1048576,uid=999,gid=999,mode=0700", "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16777216,uid=999,gid=999,mode=0700", "--env=POSTGRES_PASSWORD=FictionalDatabaseOnly1!", serviceDatabaseImage)
	if err != nil {
		return err
	}
	if _, err = run(30*time.Second, "docker", "start", db); err != nil {
		return fmt.Errorf("fixture database start failed")
	}
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err = run(10*time.Second, "docker", "exec", db, "pg_isready", "--host=127.0.0.1", "--port=5432", "--username=postgres"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("fixture database not ready")
		}
		time.Sleep(time.Second)
	}
	args := []string{"--pull=never", "--platform=linux/amd64", "--network=" + network, "--user=65532:65532", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--cpus=2", "--tmpfs=/tmp:" + serviceTmpfs}
	environment := []string{
		"ZITADEL_MASTERKEY=00000000000000000000000000000000", "ZITADEL_EXTERNALDOMAIN=fixture.invalid", "ZITADEL_EXTERNALPORT=8080", "ZITADEL_EXTERNALSECURE=false", "ZITADEL_TLS_ENABLED=false",
		"ZITADEL_DATABASE_POSTGRES_HOST=database", "ZITADEL_DATABASE_POSTGRES_DATABASE=zitadel", "ZITADEL_DATABASE_POSTGRES_USER_USERNAME=zitadel", "ZITADEL_DATABASE_POSTGRES_USER_PASSWORD=FictionalServiceOnly1!", "ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD=FictionalDatabaseOnly1!", "ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE=disable", "ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE=disable",
		"ZITADEL_FIRSTINSTANCE_SKIP=false", "ZITADEL_FIRSTINSTANCE_INSTANCENAME=Fictional Local Fixture", "ZITADEL_FIRSTINSTANCE_ORG_NAME=Fixture", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_USERNAME=operator@example.invalid", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_ADDRESS=operator@example.invalid", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_VERIFIED=true", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD=FictionalOperatorOnly1!", "ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORDCHANGEREQUIRED=false",
		"ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED=false", "ZITADEL_DEFAULTINSTANCE_LOGINPOLICY_ALLOWREGISTER=false", "ZITADEL_DEFAULTINSTANCE_LOGINPOLICY_ALLOWEXTERNALIDP=false", "ZITADEL_DEFAULTINSTANCE_DOMAINPOLICY_USERLOGINMUSTBEDOMAIN=false", "ZITADEL_TELEMETRY_ENABLED=false", "ZITADEL_SERVICEPING_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_WEBHOOK_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_PRIVATEIP_ENABLED=false", "ZITADEL_MACHINE_IDENTIFICATION_HOSTNAME_ENABLED=true",
	}
	for _, e := range environment {
		args = append(args, "--env="+e)
	}
	args = append(args, image)
	args = append(args, serviceArgs...)
	service, err := create(args...)
	if err != nil {
		return err
	}
	inspect := func(status string) (imageContainer, error) {
		b, e := run(30*time.Second, "docker", "inspect", service)
		if e != nil {
			return imageContainer{}, e
		}
		c, e := serviceInspect(b, service, image, owner, network, status)
		if e != nil {
			return c, e
		}
		seen := map[string]string{}
		for _, entry := range c.Config.Env {
			key, value, ok := strings.Cut(entry, "=")
			if !ok {
				return imageContainer{}, fmt.Errorf("malformed fixture environment")
			}
			if strings.HasPrefix(key, "ZITADEL_") {
				if _, exists := seen[key]; exists {
					return imageContainer{}, fmt.Errorf("duplicate fixture environment")
				}
				seen[key] = value
			}
		}
		if len(seen) != len(environment) {
			return imageContainer{}, fmt.Errorf("unexpected fixture environment")
		}
		for _, entry := range environment {
			key, value, _ := strings.Cut(entry, "=")
			if seen[key] != value {
				return imageContainer{}, fmt.Errorf("fixture environment differs")
			}
		}
		return c, nil
	}
	if _, err = inspect("created"); err != nil {
		return err
	}
	started := time.Now()
	if _, err = run(30*time.Second, "docker", "start", service); err != nil {
		return fmt.Errorf("identity service start failed")
	}
	c, err := inspect("running")
	if err != nil {
		return err
	}
	target, err := serviceTarget(c, network)
	if err != nil {
		return err
	}
	base := "http://" + serviceAuthority
	client := &http.Client{Timeout: 5 * time.Second, Transport: serviceTransport(target), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "http" || req.URL.Host != serviceAuthority {
			return fmt.Errorf("fixture redirect escaped")
		}
		return nil
	}}
	defer client.CloseIdleConnections()
	get := func(path string) ([]byte, error) {
		return serviceGet(client, base, path)
	}
	deadline = time.Now().Add(3 * time.Minute)
	for {
		if _, err = get("/debug/ready"); err == nil {
			break
		}
		if _, e := inspect("running"); e != nil {
			return fmt.Errorf("service readiness inspection failed: %w", e)
		}
		if time.Now().After(deadline) {
			// This diagnostic is never an alternate success path. It separates
			// application readiness from the host's published-port path, and
			// discards all command output before reporting a boolean.
			_, insideErr := run(10*time.Second, "docker", "exec", service, "/app/zitadel", "ready")
			return fmt.Errorf("identity service did not become ready: %w (inContainerReady=%t)", err, insideErr == nil)
		}
		time.Sleep(2 * time.Second)
	}
	readyMs := time.Since(started).Milliseconds()
	b, err = get("/.well-known/openid-configuration")
	if err != nil {
		return err
	}
	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err = json.Unmarshal(b, &discovery); err != nil || discovery.Issuer != base || discovery.AuthorizationEndpoint != base+"/oauth/v2/authorize" || discovery.TokenEndpoint != base+"/oauth/v2/token" {
		return fmt.Errorf("actual OIDC discovery differs")
	}
	b, err = get("/ui/console/")
	if err != nil || !strings.Contains(string(b), "<cnsl-root>") {
		return fmt.Errorf("actual embedded console unavailable")
	}
	processes := []serviceProcess{}
	for sample := 0; sample < 3; sample++ {
		c, e := inspect("running")
		if e != nil {
			return e
		}
		top, e := run(30*time.Second, "docker", "top", service, "-eo", "pid,ppid,uid,gid,comm")
		if e != nil {
			return e
		}
		lines := strings.Split(strings.TrimSpace(string(top)), "\n")
		if len(lines) != 2 {
			return fmt.Errorf("unexpected service process inventory")
		}
		f := strings.Fields(lines[1])
		if len(f) != 5 || f[0] != strconv.Itoa(c.State.Pid) || f[2] != "65532" || f[3] != "65532" || f[4] != "zitadel" {
			return fmt.Errorf("service process inventory differs")
		}
		b, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(c.State.Pid), "status"))
		if e != nil {
			return fmt.Errorf("native Linux process evidence unavailable")
		}
		process, e := serviceProcessStatus(b, c.State.Pid)
		if e != nil {
			return e
		}
		mountInfo, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(c.State.Pid), "mountinfo"))
		if e != nil {
			return fmt.Errorf("native Linux mount evidence unavailable")
		}
		process.Mounts, e = serviceMountStatus(mountInfo)
		if e != nil {
			return e
		}
		processes = append(processes, process)
		if sample < 2 {
			time.Sleep(time.Second)
		}
	}
	b, err = run(30*time.Second, "docker", "network", "inspect", network)
	if err != nil {
		return err
	}
	if err = imageDecode(b, &networks); err != nil || len(networks) != 1 || !networks[0].Internal || networks[0].ID != network || networks[0].Labels[label] != owner || len(networks[0].Containers) != 2 || networks[0].Containers[service] == nil || networks[0].Containers[db] == nil {
		return fmt.Errorf("running fixture network differs")
	}
	if _, err = run(time.Minute, "docker", "stop", "--time=20", service); err != nil {
		return fmt.Errorf("service shutdown failed")
	}
	if _, err = inspect("exited"); err != nil {
		return err
	}
	after, err := imageRead(repo, "scripts/zitadel-service-smoke.go", 1<<20)
	if err != nil || imageHash(after) != imageHash(source) {
		return fmt.Errorf("service harness changed during execution")
	}
	if err = cleanup(); err != nil {
		return err
	}
	cleaned = true
	receipt := struct {
		SchemaVersion       int              `json:"schemaVersion"`
		Profile             string           `json:"profile"`
		UpstreamCommit      string           `json:"upstreamCommit"`
		LocalImageID        string           `json:"localImageId"`
		ServiceSourceSHA256 string           `json:"serviceSourceSha256"`
		DatabaseImage       string           `json:"databaseImage"`
		ReadyMilliseconds   int64            `json:"readyMilliseconds"`
		Processes           []serviceProcess `json:"processSamples"`
		ReadOnlyRoot        bool             `json:"readOnlyRoot"`
		WritablePaths       []string         `json:"configuredTmpfsPaths"`
		Network             string           `json:"network"`
		HTTP                string           `json:"transport"`
		Discovery           bool             `json:"oidcDiscovery"`
		Console             bool             `json:"embeddedConsoleHTTP"`
		BrowserProof        bool             `json:"browserProof"`
		AuthenticationProof bool             `json:"authenticationProof"`
		Cleanup             string           `json:"cleanup"`
		ImageAdmission      string           `json:"imageAdmission"`
		LicenseClosure      string           `json:"licenseClosure"`
	}{1, "isolated-service-smoke", imagePin, image, imageHash(source), serviceDatabaseImage, readyMs, processes, true, []string{"/tmp"}, "internal-two-container-direct", "fictional-http-only", true, true, false, false, "complete", "unknown", "unknown"}
	b, err = json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "service-smoke.json", append(b, '\n'), 0444)
}
