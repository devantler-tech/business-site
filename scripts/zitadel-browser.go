package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	imagepng "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type browserPage struct {
	URL           string   `json:"url"`
	Ready         string   `json:"ready"`
	Text          string   `json:"text"`
	Username      bool     `json:"username"`
	Password      bool     `json:"password"`
	Console       bool     `json:"console"`
	Presented     bool     `json:"consolePresented"`
	OptionalMFA   bool     `json:"optionalMFA"`
	MFAAction     string   `json:"mfaAction"`
	Font          string   `json:"font"`
	FontsReady    bool     `json:"fontsReady"`
	FaceLoaded    bool     `json:"selectedFaceLoaded"`
	SecureContext bool     `json:"secureContext"`
	WebCrypto     bool     `json:"webCrypto"`
	FailedFonts   []string `json:"failedFonts"`
	Resources     []string `json:"resources"`
}

// browserAdmit requires one complete native-page observation, including its
// rendered form, actually loaded font and confined resources.
func browserAdmit(page browserPage, kind string) bool {
	u, err := url.Parse(page.URL)
	if err != nil || u.Scheme != "http" || u.Host != serviceAuthority || u.User != nil || u.Opaque != "" ||
		page.Ready != "complete" || strings.TrimSpace(page.Text) == "" || strings.TrimSpace(page.Font) == "" ||
		strings.Contains(strings.ToLower(page.Font), "ailerons") || !page.FontsReady || !page.FaceLoaded ||
		!page.SecureContext || !page.WebCrypto || page.FailedFonts == nil || len(page.FailedFonts) > 0 || len(page.Resources) == 0 {
		return false
	}
	for _, resource := range page.Resources {
		u, e := url.Parse(resource)
		if e != nil || u.Scheme != "http" || u.Host != serviceAuthority || u.User != nil || u.Opaque != "" || strings.Contains(strings.ToLower(resource), "ailerons") {
			return false
		}
	}
	if kind == "console" {
		return strings.HasPrefix(u.Path, "/ui/console/") && page.Console && page.Presented && len(strings.TrimSpace(page.Text)) > 40
	}
	if !strings.HasPrefix(u.Path, "/ui/login/") {
		return false
	}
	if kind == "optional-mfa" {
		action, err := url.Parse(page.MFAAction)
		return err == nil && page.OptionalMFA && action.Scheme == "http" && action.Host == serviceAuthority && action.User == nil && action.Opaque == "" && action.Path == "/ui/login/mfa/prompt"
	}
	return (kind == "login" && page.Username) || (kind == "password" && page.Password)
}

// The script runs inside the native page through WebDriver. Host orchestration
// and evidence admission remain Go; no page or upstream source is modified.
const browserObservation = `
if (!document.body) return null;
const root = document.querySelector('cnsl-root');
const nav = root?.querySelector('cnsl-nav');
const settled = !!nav && nav.getAnimations({subtree:true}).every(a=>!a.pending && (a.playState==='finished' || a.playState==='idle'));
const navigationVisible = !!nav && [...nav.querySelectorAll('a.nav-item')].some(a=>{
  const r=a.getBoundingClientRect();
  return r.width>0 && r.height>=20 && r.top>=0 && r.bottom<=innerHeight && r.left>=0 && r.right<=innerWidth &&
    document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.closest('a')===a;
});
const shown = selector => [...document.querySelectorAll(selector)].some(e => e.getClientRects().length);
const skip = [...document.querySelectorAll('button[name="skip"][value="true"][formnovalidate]')].find(e=>e.getClientRects().length && e.form?.method==='post');
const sample = document.querySelector('h1') || document.body;
const font = getComputedStyle(sample).fontFamily;
return {url:location.href, ready:document.readyState, text:document.body.innerText,
  secureContext:window.isSecureContext, webCrypto:typeof crypto.subtle?.digest === 'function',
  username:shown('#loginName'), password:shown('#password'),
  console:!!(root && root.querySelector('cnsl-header') && root.querySelector('cnsl-nav')),
  consolePresented:settled && navigationVisible,
  optionalMFA:!!skip, mfaAction:skip?.form?.action || '',
  font, fontsReady:document.fonts.status === 'loaded',
  selectedFaceLoaded:[...document.fonts].some(f=>f.status==='loaded' &&
    f.family.replaceAll('"','').replaceAll("'",'')===font.split(',')[0].trim().replaceAll('"','').replaceAll("'",'')),
  failedFonts:[...document.fonts].filter(f=>f.status==='error').map(f=>f.family),
  resources:performance.getEntriesByType('resource').map(e=>e.name)};`

type browserDriver struct {
	ctx               context.Context
	client            *http.Client
	session, category string
	mfaDeferred       bool
	lastPage          browserPage
	sessionIDLength   int
	versionPresent    bool
}

// browserHTTPClient bounds driver requests beyond the configured page-load
// deadline and refuses proxies and redirects outside the local driver endpoint.
func browserHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("WebDriver redirect refused") }}
}

// request consumes a bounded complete WebDriver response before decoding it.
// Driver messages and page content never become public diagnostics.
func (d *browserDriver) request(method, path string, body any, result any) error {
	d.category = "observation"
	var data io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		data = bytes.NewReader(b)
	}
	ctx := d.ctx
	if path == "/status" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:9515"+path, data)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(req)
	if err != nil {
		d.category = "transport"
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			d.category = "transport-timeout"
		}
		return fmt.Errorf("WebDriver transport failed")
	}
	b, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(b) == 4<<20 {
		d.category = "incomplete-response"
		return fmt.Errorf("WebDriver response incomplete")
	}
	var envelope struct {
		Value json.RawMessage `json:"value"`
	}
	if imageDecode(b, &envelope) != nil || len(envelope.Value) == 0 {
		d.category = "invalid-envelope"
		return fmt.Errorf("WebDriver response invalid")
	}
	var failure struct {
		Error string `json:"error"`
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(envelope.Value, &fields)
	_, hasError := fields["error"]
	if hasError {
		_ = json.Unmarshal(envelope.Value, &failure)
	}
	if response.StatusCode != http.StatusOK || hasError {
		switch failure.Error {
		case "session not created", "timeout", "no such element", "invalid argument", "javascript error":
			d.category = strings.ReplaceAll(failure.Error, " ", "-")
		default:
			d.category = "webdriver"
		}
		return fmt.Errorf("WebDriver operation failed")
	}
	if result == nil {
		return nil
	}
	if err := imageDecode(envelope.Value, result); err != nil {
		d.category = "invalid-result"
		return err
	}
	return nil
}

// call confines a command to the one browser session created by this worker.
func (d *browserDriver) call(method, path string, body, result any) error {
	return d.request(method, "/session/"+d.session+path, body, result)
}

// waitPage observes rendering until the worker deadline; errors never pass.
func (d *browserDriver) waitPage(kind string) (browserPage, error) {
	for {
		var page browserPage
		if err := d.call("POST", "/execute/sync", map[string]any{"script": browserObservation, "args": []any{}}, &page); err != nil {
			return page, err
		}
		d.lastPage = page
		if browserAdmit(page, kind) {
			return page, nil
		}
		if kind == "console" && !d.mfaDeferred && browserAdmit(page, "optional-mfa") {
			button, err := d.element(`button[name="skip"][value="true"][formnovalidate]`)
			if err != nil {
				return page, err
			}
			if err = d.call("POST", button+"/click", map[string]any{}, nil); err != nil {
				return page, err
			}
			d.mfaDeferred = true
			continue
		}
		select {
		case <-d.ctx.Done():
			return page, fmt.Errorf("browser page incomplete")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// element selects a native form control using its frozen upstream selector.
func (d *browserDriver) element(selector string) (string, error) {
	var result map[string]string
	if err := d.call("POST", "/element", map[string]string{"using": "css selector", "value": selector}, &result); err != nil {
		return "", err
	}
	id := result["element-6066-11e4-a52e-4f735466cecf"]
	if id == "" || len(id) > 512 {
		return "", fmt.Errorf("native form control absent")
	}
	return "/element/" + url.PathEscape(id), nil
}

type browserScreen struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type browserObserved struct {
	Kind          string        `json:"kind"`
	Font          string        `json:"font"`
	Loaded        bool          `json:"loaded"`
	SameOrigin    bool          `json:"sameOriginResources"`
	FontAbsent    bool          `json:"unadmittedFontAbsent"`
	SecureContext bool          `json:"secureContext"`
	WebCrypto     bool          `json:"webCrypto"`
	Presented     bool          `json:"presentationReady"`
	Screenshot    browserScreen `json:"screenshot"`
}

// capture retains a complete PNG and seals it to the accepted page observation.
func (d *browserDriver) capture(output, kind string, page browserPage) (browserObserved, error) {
	var encoded string
	if err := d.call("GET", "/screenshot", nil, &encoded); err != nil {
		return browserObserved{}, err
	}
	if len(encoded) > 4<<20 {
		return browserObserved{}, fmt.Errorf("screenshot too large")
	}
	png, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(png) < 8 || !bytes.Equal(png[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return browserObserved{}, fmt.Errorf("invalid screenshot")
	}
	config, err := imagepng.DecodeConfig(bytes.NewReader(png))
	if err != nil || config.Width < 320 || config.Height < 200 || config.Width > 4096 || config.Height > 2160 {
		return browserObserved{}, fmt.Errorf("invalid screenshot dimensions")
	}
	if _, err = imagepng.Decode(bytes.NewReader(png)); err != nil {
		return browserObserved{}, fmt.Errorf("incomplete screenshot")
	}
	if err = imageWrite(output, kind+".png", png, 0600); err != nil {
		return browserObserved{}, err
	}
	return browserObserved{Kind: kind, Font: page.Font, Loaded: true, SameOrigin: true, FontAbsent: true, SecureContext: page.SecureContext, WebCrypto: page.WebCrypto, Presented: kind != "console" || page.Presented, Screenshot: browserScreen{kind + ".png", len(png), imageHash(png)}}, nil
}

// This internal worker runs only in the already-admitted service network
// namespace. It uses the runner's matching Chrome and WebDriver without adding
// a browser library to the distribution or disabling native browser protections.
func browserWorker(output, namespace, sourceSHA string) (result error) {
	phase := "admission"
	d := browserDriver{category: "observation"}
	admitted := false
	defer func() {
		if result != nil {
			if admitted {
				// Only fixed labels, counts and booleans survive a failed observation. Never
				// retain DOM text, URLs, form values or driver diagnostics.
				page := d.lastPage
				b, err := json.MarshalIndent(map[string]any{"result": "incomplete", "stage": phase, "category": d.category, "sessionIdLength": d.sessionIDLength, "browserVersionPresent": d.versionPresent, "usernameForm": page.Username, "passwordForm": page.Password, "authenticatedConsole": page.Console, "optionalMFAOffered": page.OptionalMFA, "optionalMFADeferred": d.mfaDeferred, "selectedFontLoaded": page.FaceLoaded, "fontsReady": page.FontsReady, "failedFontCount": len(page.FailedFonts), "secureContext": page.SecureContext, "webCrypto": page.WebCrypto}, "", "  ")
				if err == nil {
					err = imageWrite(output, "browser-incomplete.json", append(b, '\n'), 0600)
				}
				if err != nil {
					result = fmt.Errorf("browser stage=%s category=%s; incomplete observation diagnostic unavailable", phase, d.category)
					return
				}
			}
			result = fmt.Errorf("browser stage=%s category=%s", phase, d.category)
		}
	}()
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil || os.Getuid() <= 0 || current != namespace || !regexp.MustCompile(`^net:\[\d+\]$`).MatchString(namespace) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(sourceSHA) {
		return fmt.Errorf("owned namespace required")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := imageRead(filepath.Dir(exe), filepath.Base(exe), 64<<20)
	if err != nil {
		return err
	}
	admitted = true
	profile, err := os.MkdirTemp("", "zitadel-browser-")
	if err != nil {
		return err
	}
	info, err := os.Lstat(profile)
	if err != nil {
		return err
	}
	profileCleaned := false
	cleanupProfile := func() error {
		after, e := os.Lstat(profile)
		if e != nil || !os.SameFile(info, after) {
			return fmt.Errorf("profile ownership changed")
		}
		if e = os.RemoveAll(profile); e != nil {
			return e
		}
		profileCleaned = true
		return nil
	}
	defer func() {
		if !profileCleaned {
			if e := cleanupProfile(); e != nil {
				result = e
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	d.ctx = ctx
	d.client = browserHTTPClient()
	defer d.client.CloseIdleConnections()
	phase = "driver"
	command := exec.Command("chromedriver", "--port=9515", "--allowed-ips=127.0.0.1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = command.Start(); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	sessionClosed, exited := false, false
	defer func() {
		category := d.category
		defer func() { d.category = category }()
		if d.session != "" {
			sessionClosed = d.call("DELETE", "", nil, nil) == nil
		}
		select {
		case <-wait:
			exited = true
		default:
		}
		if !exited {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			select {
			case <-wait:
				exited = true
			case <-time.After(2 * time.Second):
			}
		}
		if !exited {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			select {
			case <-wait:
				exited = true
			case <-time.After(2 * time.Second):
			}
		}
		if !exited || (d.session != "" && !sessionClosed) {
			result = fmt.Errorf("browser cleanup incomplete")
		}
	}()
	start := time.Now()
	for {
		var status struct {
			Ready bool `json:"ready"`
		}
		if d.request("GET", "/status", nil, &status) == nil && status.Ready {
			break
		}
		if time.Since(start) > 10*time.Second {
			return fmt.Errorf("driver startup failed")
		}
		select {
		case <-wait:
			exited = true
			return fmt.Errorf("driver exited")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	phase = "session"
	var session struct {
		ID           string `json:"sessionId"`
		Capabilities struct {
			Version string `json:"browserVersion"`
		} `json:"capabilities"`
	}
	options := map[string]any{"capabilities": map[string]any{"alwaysMatch": map[string]any{"browserName": "chrome", "goog:chromeOptions": map[string]any{"args": []string{"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--window-size=1280,900", "--user-data-dir=" + profile, "--host-resolver-rules=MAP fixture.localhost 127.0.0.1"}}}}}
	if err = d.request("POST", "/session", options, &session); err != nil {
		return err
	}
	d.sessionIDLength, d.versionPresent = len(session.ID), session.Capabilities.Version != ""
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(session.ID) {
		d.category = "invalid-session"
		return fmt.Errorf("invalid session")
	}
	d.session = session.ID
	if !d.versionPresent {
		d.category = "missing-browser-version"
		return fmt.Errorf("browser version absent")
	}
	if err = d.call("POST", "/timeouts", map[string]int{"implicit": 0, "pageLoad": 20000, "script": 10000}, nil); err != nil {
		return err
	}
	phase = "login"
	if err = d.call("POST", "/url", map[string]string{"url": "http://" + serviceAuthority + "/ui/console/"}, nil); err != nil {
		return err
	}
	pages := []browserObserved{}
	for _, kind := range []string{"login", "password", "console"} {
		phase = kind
		page, e := d.waitPage(kind)
		if e != nil {
			return e
		}
		observed, e := d.capture(output, kind, page)
		if e != nil {
			return e
		}
		pages = append(pages, observed)
		if kind == "console" {
			break
		}
		selector, text := "#loginName", "operator@example.invalid"
		if kind == "password" {
			selector, text = "#password", "FictionalOperatorOnly1!"
		}
		element, e := d.element(selector)
		if e != nil {
			return e
		}
		if e = d.call("POST", element+"/value", map[string]string{"text": text}, nil); e != nil {
			return e
		}
		button, e := d.element("#submit-button")
		if e != nil {
			return e
		}
		if e = d.call("POST", button+"/click", map[string]any{}, nil); e != nil {
			return e
		}
	}
	// Close before emitting success, rather than relying on a deferred best effort.
	phase = "cleanup"
	if err = d.call("DELETE", "", nil, nil); err != nil {
		return err
	}
	sessionClosed = true
	d.session = ""
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	select {
	case <-wait:
		exited = true
	case <-time.After(2 * time.Second):
		return fmt.Errorf("driver did not exit")
	}
	if err = cleanupProfile(); err != nil {
		return err
	}
	phase = "receipt"
	receipt := map[string]any{"schemaVersion": 1, "profile": "fictional-native-browser", "browserSourceSha256": sourceSHA, "workerExecutableSha256": imageHash(binary), "browserVersion": session.Capabilities.Version, "networkNamespace": namespace, "browserSandboxDisabled": false, "pages": pages, "optionalMFADeferred": d.mfaDeferred, "mfaProof": false, "cleanup": "complete", "portalFederation": false, "productionTLS": false, "invitationProof": false, "imageAdmission": "unknown", "licenseClosure": "unknown"}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "browser-smoke.json", append(b, '\n'), 0600)
}
