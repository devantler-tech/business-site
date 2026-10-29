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
	URL             string   `json:"url"`
	Ready           string   `json:"ready"`
	Text            string   `json:"text"`
	Username        bool     `json:"username"`
	Password        bool     `json:"password"`
	Console         bool     `json:"console"`
	ProviderConsole bool     `json:"providerConsole"`
	Presented       bool     `json:"consolePresented"`
	OptionalMFA     bool     `json:"optionalMFA"`
	MFAAction       string   `json:"mfaAction"`
	Font            string   `json:"font"`
	FontsReady      bool     `json:"fontsReady"`
	FaceLoaded      bool     `json:"selectedFaceLoaded"`
	SecureContext   bool     `json:"secureContext"`
	WebCrypto       bool     `json:"webCrypto"`
	FailedFonts     []string `json:"failedFonts"`
	Resources       []string `json:"resources"`
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
	if kind == "provider-console" {
		settings := u.Path == "/ui/console/instance" && u.Query().Get("id") == "idp"
		editor := regexp.MustCompile(`^/ui/console/instance/provider/(google/create|(google|github)/[0-9]{1,30})$`).MatchString(u.Path)
		return page.ProviderConsole && (settings || editor) && len(strings.TrimSpace(page.Text)) > 40
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
  providerConsole:!!(root && shown('cnsl-header') && shown('cnsl-instance,cnsl-provider-google,cnsl-provider-github')),
  consolePresented:settled && navigationVisible,
  optionalMFA:!!skip, mfaAction:skip?.form?.action || '',
  font, fontsReady:document.fonts.status === 'loaded',
  selectedFaceLoaded:[...document.fonts].some(f=>f.status==='loaded' &&
    f.family.replaceAll('"','').replaceAll("'",'')===font.split(',')[0].trim().replaceAll('"','').replaceAll("'",'')),
  failedFonts:[...document.fonts].filter(f=>f.status==='error').map(f=>f.family),
  resources:performance.getEntriesByType('resource').map(e=>e.name)};`

type browserDriver struct {
	ctx                                                     context.Context
	client                                                  *http.Client
	session, category                                       string
	mfaDeferred                                             bool
	lastPage                                                browserPage
	sessionIDLength                                         int
	versionPresent                                          bool
	providerStage                                           string
	providerSetupPhase, providerRequest, providerHTTPStatus int
	lastProvider                                            browserProviderPage
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
				b, err := json.MarshalIndent(map[string]any{"result": "incomplete", "stage": phase, "category": d.category, "providerStage": d.providerStage, "providerSetupPhase": d.providerSetupPhase, "providerRequest": d.providerRequest, "providerHTTPStatus": d.providerHTTPStatus, "providerCount": d.lastProvider.Count, "providerGlyphs": d.lastProvider.Glyphs, "providerLabels": d.lastProvider.Labels, "providerTargets": d.lastProvider.Targets, "providerThemeReady": d.lastProvider.ThemeReady, "providerResources": d.lastProvider.Resources, "providerKeyboard": d.lastProvider.Keyboard, "sessionIdLength": d.sessionIDLength, "browserVersionPresent": d.versionPresent, "usernameForm": page.Username, "passwordForm": page.Password, "authenticatedConsole": page.Console, "optionalMFAOffered": page.OptionalMFA, "optionalMFADeferred": d.mfaDeferred, "selectedFontLoaded": page.FaceLoaded, "fontsReady": page.FontsReady, "failedFontCount": len(page.FailedFonts), "secureContext": page.SecureContext, "webCrypto": page.WebCrypto}, "", "  ")
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
			sessionClosed = d.closeSession() == nil
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
	// Provider presentation follows successful native operator authentication. Its
	// separate deadline does not relax the existing operator or WebDriver budgets.
	phase = "providers"
	providerCtx, providerCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer providerCancel()
	d.ctx = providerCtx
	providers, err := d.providerPresentation(output)
	if err != nil {
		return err
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
	receipt := map[string]any{"schemaVersion": 1, "profile": "fictional-native-browser", "browserSourceSha256": sourceSHA, "workerExecutableSha256": imageHash(binary), "browserVersion": session.Capabilities.Version, "networkNamespace": namespace, "browserSandboxDisabled": false, "pages": pages, "providerIcons": providers, "optionalMFADeferred": d.mfaDeferred, "mfaProof": false, "cleanup": "complete", "portalFederation": false, "productionTLS": false, "invitationProof": false, "imageAdmission": "unknown", "licenseClosure": "unknown"}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return imageWrite(output, "browser-smoke.json", append(b, '\n'), 0600)
}

// Only fixed labels and successful observation booleans enter the public receipt.
// The page URL (which can contain an auth request) stays in worker memory.
type browserProviderPage struct {
	URL        string `json:"url"`
	Kind       string `json:"kind"`
	Theme      string `json:"theme"`
	Count      int    `json:"count"`
	Glyphs     bool   `json:"glyphs"`
	Labels     bool   `json:"labels"`
	Targets    bool   `json:"targets"`
	ThemeReady bool   `json:"themeReady"`
	Resources  bool   `json:"resources"`
	Keyboard   bool   `json:"keyboard"`
}
type browserProviderCase struct {
	Kind       string        `json:"kind"`
	Theme      string        `json:"theme"`
	Count      int           `json:"count"`
	Glyphs     bool          `json:"glyphs"`
	Labels     bool          `json:"labels"`
	Targets    bool          `json:"targets"`
	ThemeReady bool          `json:"themeReady"`
	Resources  bool          `json:"resources"`
	Keyboard   bool          `json:"keyboard"`
	Screenshot browserScreen `json:"screenshot"`
}

// browserProviderAdmit requires complete provider observations on the fixed local
// route for each case, with its expected theme, icon count and native controls.
func browserProviderAdmit(p browserProviderPage, kind, theme string) bool {
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "http" || u.Host != serviceAuthority || u.User != nil || u.Opaque != "" || p.Kind != kind || p.Theme != theme || (theme != "light" && theme != "dark") || !p.Glyphs || !p.Labels || !p.Targets || !p.ThemeReady || !p.Resources || !p.Keyboard {
		return false
	}
	switch kind {
	case "picker":
		return p.Count == 9 && u.Path == "/ui/console/instance" && u.Query().Get("id") == "idp"
	case "table":
		return p.Count == 2 && u.Path == "/ui/console/instance" && u.Query().Get("id") == "idp"
	case "editor-google":
		return p.Count == 1 && u.Path == "/ui/console/instance/provider/google/create"
	case "login":
		return p.Count == 2 && u.Path == "/ui/login/login"
	case "registration":
		return p.Count == 2 && u.Path == "/ui/login/register/option"
	}
	return false
}

// This runs only after the native fictional operator has authenticated. Its
// short-lived bearer remains inside that page, and is neither returned nor logged.
// All requests are fixed same-origin native APIs; no provider endpoint is called.
const browserProviderSetup = `
const done=arguments[arguments.length-1];
(async()=>{
 let phase=0,request=0,status=0;
 try {
  if(location.origin!=='http://fixture.localhost:8080'||!location.pathname.startsWith('/ui/console/'))throw 0;
  const token=JSON.parse(sessionStorage.getItem('zitadel:access_token'));
  if(typeof token!=='string'||!token||token.length>16384)throw 0;
  const api=async(method,path,body)=>{
   request++;status=0;
   const timer=new AbortController();const stop=setTimeout(()=>timer.abort(),2000);
   try {
    const response=await fetch(path,{method,headers:{'Content-Type':'application/json','Authorization':'Bearer '+token},body:body===undefined?undefined:JSON.stringify(body),cache:'no-store',redirect:'error',signal:timer.signal});
    status=Number.isInteger(response.status)?response.status:0;
    if(!response.ok||!response.headers.get('content-type')?.includes('application/json'))throw 0;
    const text=await response.text();if(!text||text.length>1048576)throw 0;const value=JSON.parse(text);if(!value||typeof value!=='object'||Array.isArray(value))throw 0;return value;
   }finally{clearTimeout(stop)}
  };
  const bools=['allowUsernamePassword','allowRegister','allowExternalIdp','forceMfa','hidePasswordReset','ignoreUnknownUsernames','allowDomainDiscovery','disableLoginWithEmail','disableLoginWithPhone','forceMfaLocalOnly'];
  const durations=['passwordCheckLifetime','externalLoginCheckLifetime','mfaInitSkipLifetime','secondFactorCheckLifetime','multiFactorCheckLifetime'];
  const normalize=p=>{
   if(!p||typeof p!=='object'||Array.isArray(p))throw 0;
   const out={};for(const k of bools){if(k in p&&typeof p[k]!=='boolean')throw 0;out[k]=p[k]??false}
   for(const k of durations){if(k in p&&(typeof p[k]!=='string'||!/^\d+(\.\d{1,9})?s$/.test(p[k])))throw 0;out[k]=p[k]??'0s'}
   if('defaultRedirectUri'in p&&typeof p.defaultRedirectUri!=='string')throw 0;out.defaultRedirectUri=p.defaultRedirectUri??'';
   if('passwordlessType'in p&&!['PASSWORDLESS_TYPE_NOT_ALLOWED','PASSWORDLESS_TYPE_ALLOWED'].includes(p.passwordlessType))throw 0;out.passwordlessType=p.passwordlessType??'PASSWORDLESS_TYPE_NOT_ALLOWED';return out;
  };
  const before=(await api('GET','/admin/v1/policies/login')).policy;
  const intended=normalize(before);
  if(!intended.allowUsernamePassword||intended.allowRegister||(before.idps??[]).length!==0)throw 0;
  const stable=p=>JSON.stringify([p.secondFactors??[],p.multiFactors??[],p.isDefault??false]);const stableBefore=stable(before);
  phase=1;
  const providers=[['google','Fixture Google','fictional-google-client','FictionalGoogleOnly1!',['openid','profile','email']],['github','Fixture GitHub','fictional-github-client','FictionalGitHubOnly1!',['read:user']]];
  const ids=[];
  for(const [type,name,clientId,clientSecret,scopes]of providers){
   const created=await api('POST','/admin/v1/idps/'+type,{name,clientId,clientSecret,scopes,providerOptions:{isLinkingAllowed:false,isCreationAllowed:false,isAutoCreation:false,isAutoUpdate:false,autoLinking:'AUTO_LINKING_OPTION_UNSPECIFIED'}});
   if(typeof created.id!=='string'||!/^\d{1,30}$/.test(created.id)||ids.includes(created.id))throw 0;
   ids.push(created.id);
   const read=await api('GET','/admin/v1/idps/templates/'+created.id);
   if(read.idp?.id!==created.id||read.idp?.name!==name)throw 0;
  }
  phase=2;intended.allowExternalIdp=true;await api('PUT','/admin/v1/policies/login',intended);
  let after=(await api('GET','/admin/v1/policies/login')).policy;
  if(JSON.stringify(normalize(after))!==JSON.stringify(intended)||stable(after)!==stableBefore)throw 0;
  phase=3;
  for(const idpId of ids)await api('POST','/admin/v1/policies/login/idps',{idpId});
  const read=await api('POST','/admin/v1/policies/login/idps/_search',{});
  const valid=list=>Array.isArray(list)&&list.length===2&&ids.every((id,i)=>list.some(p=>p.idpId===id&&p.idpName===providers[i][1]));
  after=(await api('GET','/admin/v1/policies/login')).policy;
  if(String(read.details?.totalResult)!=='2'||!valid(read.result)||!valid(after.idps)||JSON.stringify(normalize(after))!==JSON.stringify(intended)||stable(after)!==stableBefore)throw 0;
  done({complete:true,count:ids.length,phase:4,ids,request,status});
 }catch{done({complete:false,count:0,phase,request,status})}
})();`

// Read computed native glyphs, labels and targets. No DOM/style/event injection
// is used. Table evidence covers the two configured fictional variants; editor
// evidence covers Google. The closed source hashes cover the other eight editors.
const browserProviderObservation = `
const [kind,theme]=arguments;
const login=kind==='login'||kind==='registration';
const selector=login?'a.lgn-idp .logo':kind==='picker'?'cnsl-idp-settings a.item .neutral-provider-icon':kind==='table'?'cnsl-idp-table .neutral-provider-icon':'cnsl-provider-google .neutral-provider-icon';
const icons=[...document.querySelectorAll(selector)];
const shown=e=>{const r=e.getBoundingClientRect();return e.getClientRects().length>0&&r.width>0&&r.height>0&&r.top>=0&&r.left>=0&&r.bottom<=innerHeight&&r.right<=innerWidth;};
const routes=['google','azure-ad','github','github-es','gitlab','gitlab-self-hosted','apple','oauth','saml'];
const labels=['Google','Microsoft','GitHub','GitHub Enterprise Server','GitLab','GitLab Self Hosted','Apple','Generic OAuth','SAML'];
const plain=s=>s.replaceAll('"','').replaceAll("'",'').trim();
const glyphs=icons.length>0&&icons.every(e=>{
 const style=getComputedStyle(e),before=getComputedStyle(e,'::before'),rect=e.getBoundingClientRect();
 const family=login?'lgn-icons':'Line Awesome Free';const code=login?0xe905:0xf061;const size=login?46:kind==='table'?28:36;
 return shown(e)&&e.getAttribute('aria-hidden')==='true'&&!e.hasAttribute('role')&&!e.hasAttribute('tabindex')&&e.tabIndex<0&&
  Math.abs(rect.width-size)<1&&Math.abs(rect.height-size)<1&&style.backgroundImage==='none'&&
  plain(before.content).codePointAt(0)===code&&plain(before.fontFamily)===family&&
  (!login?before.fontWeight==='900':Math.abs(parseFloat(before.fontSize)-25)<1)&&
  [...document.fonts].some(f=>plain(f.family)===family&&f.status==='loaded');
});
let labelOK=false,targetOK=false,keyboard=false;
if(kind==='picker'){
 const anchors=icons.map(e=>e.closest('a'));
 labelOK=labels.every(label=>anchors.some(a=>a?.innerText.trim()===label));
 targetOK=routes.every(route=>anchors.some(a=>a&&a.origin===location.origin&&a.pathname==='/ui/console/instance/provider/'+route+'/create'));
 keyboard=anchors.every(a=>a&&a.tabIndex===0&&shown(a));
}else if(kind==='table'){
 const rows=icons.map(e=>e.closest('tr'));
 labelOK=['Fixture Google','Fixture GitHub'].every(name=>rows.some(r=>r?.innerText.includes(name)));
 targetOK=rows.every(r=>r?.classList.contains('highlight'));
 keyboard=icons.every(e=>e.tabIndex<0);
}else if(kind==='editor-google'){
 labelOK=!!document.querySelector('cnsl-provider-google input[formcontrolname="clientId"]')&&document.body.innerText.includes('Google');
 targetOK=!!document.querySelector('cnsl-provider-google input[formcontrolname="clientSecret"]')&&!!document.querySelector('cnsl-provider-google button[type="submit"]');
 keyboard=icons.every(e=>e.tabIndex<0);
}else{
 const anchors=icons.map(e=>e.closest('a.lgn-idp'));
 labelOK=['Fixture Google','Fixture GitHub'].every(name=>anchors.some(a=>a?.innerText.includes(name)));
 targetOK=anchors.every(a=>a&&a.origin===location.origin&&a.pathname===(kind==='login'?'/ui/login/login/externalidp':'/ui/login/register/externalidp'));
 keyboard=anchors.every(a=>a&&a.tabIndex===0&&shown(a));
}
const resources=performance.getEntriesByType('resource').map(e=>e.name);
return {url:location.href,kind,theme,count:icons.length,glyphs,labels:labelOK,targets:targetOK,keyboard,
 themeReady:login?document.documentElement.classList.contains('lgn-'+theme+'-theme'):!!document.querySelector('cnsl-root.'+theme+'-theme'),
 resources:document.readyState==='complete'&&document.fonts.status==='loaded'&&resources.length>0&&resources.every(name=>{const u=new URL(name);return u.origin===location.origin&&!/(?:assets\/images|resources\/images)\/idp\//i.test(u.pathname)})};`

const browserRegistrationURL = `
const id=document.querySelector('input[name="authRequestID"]')?.value;
return id&&/^[A-Za-z0-9_-]{1,128}$/.test(id)?location.origin+'/ui/login/register/option?authRequestID='+encodeURIComponent(id):'';`

// providerPage observes native controls within the provider phase's deadline,
// returning only after the full case passes admission or observation fails.
func (d *browserDriver) providerPage(kind, theme string) (browserProviderPage, error) {
	for {
		// Theme buttons are at the footer. Move only the native viewport back to
		// the observed controls; never manufacture DOM, styles, focus or events.
		var visibleTarget bool
		if err := d.call("POST", "/execute/sync", map[string]any{"script": `const selectors={picker:'cnsl-idp-settings .new-idp-wrapper',table:'cnsl-idp-table','editor-google':'cnsl-provider-google .neutral-provider-icon',login:'.lgn-idp-providers',registration:'.lgn-register-options'};const e=document.querySelector(selectors[arguments[0]]);if(!e)return false;e.scrollIntoView({behavior:'instant',block:'center'});return true;`, "args": []string{kind}}, &visibleTarget); err != nil {
			return browserProviderPage{}, err
		}
		var p browserProviderPage
		if err := d.call("POST", "/execute/sync", map[string]any{"script": browserProviderObservation, "args": []string{kind, theme}}, &p); err != nil {
			return p, err
		}
		d.lastProvider = p
		if visibleTarget && browserProviderAdmit(p, kind, theme) {
			return p, nil
		}
		select {
		case <-d.ctx.Done():
			return p, fmt.Errorf("provider presentation incomplete")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Provider setup and presentation have their own bounded 120s phase after the
// unchanged 100s operator journey. They never widen the service network namespace.
func (d *browserDriver) providerPresentation(output string) (cases []browserProviderCase, result error) {
	defer func() {
		if result != nil && d.session != "" {
			old, category := d.ctx, d.category
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			d.ctx = ctx
			_, _ = d.capture(output, "provider-incomplete", browserPage{})
			cancel()
			d.ctx = old
			d.category = category
		}
	}()
	d.providerStage = "setup"
	if err := d.call("POST", "/timeouts", map[string]int{"implicit": 0, "pageLoad": 20000, "script": 20000}, nil); err != nil {
		return nil, err
	}
	var setup struct {
		Complete bool     `json:"complete"`
		Count    int      `json:"count"`
		Phase    int      `json:"phase"`
		IDs      []string `json:"ids"`
		Request  int      `json:"request"`
		Status   int      `json:"status"`
	}
	if err := d.call("POST", "/execute/async", map[string]any{"script": browserProviderSetup, "args": []any{}}, &setup); err != nil {
		return nil, err
	}
	if setup.Phase >= 0 && setup.Phase <= 4 {
		d.providerSetupPhase = setup.Phase
	}
	if setup.Request >= 0 && setup.Request <= 11 {
		d.providerRequest = setup.Request
	}
	if setup.Status >= 0 && setup.Status <= 599 {
		d.providerHTTPStatus = setup.Status
	}
	if !setup.Complete || setup.Count != 2 || setup.Phase != 4 || setup.Request != 11 || setup.Status != 200 || len(setup.IDs) != 2 || setup.IDs[0] == setup.IDs[1] || !regexp.MustCompile("^[0-9]{1,30}$").MatchString(setup.IDs[0]) || !regexp.MustCompile("^[0-9]{1,30}$").MatchString(setup.IDs[1]) {
		return nil, fmt.Errorf("fictional provider setup incomplete")
	}
	cases = []browserProviderCase{}
	capture := func(kind, theme string) error {
		d.providerStage = kind + "-" + theme
		p, err := d.providerPage(kind, theme)
		if err != nil {
			return err
		}
		if kind == "login" || kind == "registration" {
			d.providerStage = kind + "-" + theme + "-keyboard"
			d.lastProvider.Keyboard = false
			if err = d.providerTab(); err != nil {
				return err
			}
			d.lastProvider.Keyboard = true
		}
		screen, err := d.capture(output, "provider-"+kind+"-"+theme, browserPage{})
		if err != nil {
			return err
		}
		if kind == "table" {
			d.lastProvider.Targets = false
			if err = d.providerTableRoutes(setup.IDs); err != nil {
				return err
			}
			d.lastProvider.Targets = true
		}
		cases = append(cases, browserProviderCase{kind, theme, p.Count, p.Glyphs, p.Labels, p.Targets, p.ThemeReady, p.Resources, p.Keyboard, screen.Screenshot})
		return nil
	}
	for _, theme := range []string{"light", "dark"} {
		if err := d.call("POST", "/url", map[string]string{"url": "http://" + serviceAuthority + "/ui/console/instance?id=idp"}, nil); err != nil {
			return nil, err
		}
		if _, err := d.waitPage("provider-console"); err != nil {
			return nil, err
		}
		var active bool
		selector := `cnsl-theme-setting button.theme-setting-button:has(svg.sun)`
		if theme == "dark" {
			selector = `cnsl-theme-setting button.theme-setting-button:has(svg.moon)`
		}
		if err := d.call("POST", "/execute/sync", map[string]any{"script": `return document.querySelector(arguments[0])?.classList.contains('active')===true;`, "args": []string{selector}}, &active); err != nil {
			return nil, err
		}
		if !active {
			button, err := d.element(selector)
			if err != nil {
				return nil, err
			}
			if err = d.call("POST", button+"/click", map[string]any{}, nil); err != nil {
				return nil, err
			}
		}
		for _, kind := range []string{"picker", "table"} {
			if err := capture(kind, theme); err != nil {
				return nil, err
			}
		}
		// Native keyboard activation of the existing chooser anchor, never the
		// off-host federation action, proves the route remains usable without a mouse.
		link, err := d.element(`cnsl-idp-settings a[href="/ui/console/instance/provider/google/create"]`)
		if err != nil {
			return nil, err
		}
		if err = d.call("POST", link+"/value", map[string]string{"text": "\ue007"}, nil); err != nil {
			return nil, err
		}
		if _, err = d.waitPage("provider-console"); err != nil {
			return nil, err
		}
		if err = capture("editor-google", theme); err != nil {
			return nil, err
		}
	}
	// Clear only this worker's disposable profile, after all authenticated Console
	// observations. No real user's session, provider account or persistent state exists.
	if err := d.call("POST", "/execute/sync", map[string]any{"script": `sessionStorage.clear(); localStorage.clear(); return true;`, "args": []any{}}, nil); err != nil {
		return nil, err
	}
	if err := d.call("DELETE", "/cookie", nil, nil); err != nil {
		return nil, err
	}
	for _, theme := range []string{"light", "dark"} {
		if err := d.call("POST", "/cookie", map[string]any{"cookie": map[string]any{"name": "mode", "value": theme, "path": "/"}}, nil); err != nil {
			return nil, err
		}
		if err := d.call("POST", "/url", map[string]string{"url": "http://" + serviceAuthority + "/ui/console/"}, nil); err != nil {
			return nil, err
		}
		if _, err := d.waitPage("login"); err != nil {
			return nil, err
		}
		if err := capture("login", theme); err != nil {
			return nil, err
		}
		// Derive only the auth request from the actual native login form. No external
		// provider link is followed, and local registration remains disabled.
		var registration string
		if err := d.call("POST", "/execute/sync", map[string]any{"script": browserRegistrationURL, "args": []any{}}, &registration); err != nil {
			return nil, err
		}
		u, err := url.Parse(registration)
		if err != nil || u.Scheme != "http" || u.Host != serviceAuthority || u.Path != "/ui/login/register/option" {
			return nil, fmt.Errorf("native registration request absent")
		}
		if err = d.call("POST", "/url", map[string]string{"url": registration}, nil); err != nil {
			return nil, err
		}
		if err = capture("registration", theme); err != nil {
			return nil, err
		}
	}
	return cases, nil
}

// Follow the native Tab sequence without activating either federation endpoint.
func (d *browserDriver) providerTab() error {
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		actions := map[string]any{"actions": []any{map[string]any{"type": "key", "id": "native-tab", "actions": []any{map[string]any{"type": "keyDown", "value": "\ue004"}, map[string]any{"type": "keyUp", "value": "\ue004"}}}}}
		if err := d.call("POST", "/actions", actions, nil); err != nil {
			return err
		}
		var focus string
		if err := d.call("POST", "/execute/sync", map[string]any{"script": `const e=document.activeElement;if(e?.matches('.logo'))return 'decorative';const a=e?.closest('a.lgn-idp');return a?.innerText.includes('Fixture Google')?'google':a?.innerText.includes('Fixture GitHub')?'github':'';`, "args": []any{}}, &focus); err != nil {
			return err
		}
		if focus == "decorative" {
			return fmt.Errorf("decorative provider icon took focus")
		}
		if focus == "google" || focus == "github" {
			seen[focus] = true
		}
		if len(seen) == 2 {
			return d.call("DELETE", "/actions", nil, nil)
		}
	}
	return fmt.Errorf("native provider links unreachable by Tab")
}

// The host independently admits the closed case set and every retained PNG.
func browserProviderReceipt(output string, cases []browserProviderCase) error {
	if len(cases) != 10 {
		return fmt.Errorf("provider browser case set incomplete")
	}
	order := []string{"picker/light", "table/light", "editor-google/light", "picker/dark", "table/dark", "editor-google/dark", "login/light", "registration/light", "login/dark", "registration/dark"}
	for i, c := range cases {
		if c.Kind+"/"+c.Theme != order[i] {
			return fmt.Errorf("provider browser case order differs")
		}
		route := "/ui/console/instance?id=idp"
		switch c.Kind {
		case "editor-google":
			route = "/ui/console/instance/provider/google/create"
		case "login":
			route = "/ui/login/login"
		case "registration":
			route = "/ui/login/register/option"
		}
		observation := browserProviderPage{"http://" + serviceAuthority + route, c.Kind, c.Theme, c.Count, c.Glyphs, c.Labels, c.Targets, c.ThemeReady, c.Resources, c.Keyboard}
		if !browserProviderAdmit(observation, c.Kind, c.Theme) {
			return fmt.Errorf("provider browser case incomplete")
		}
		name := "provider-" + c.Kind + "-" + c.Theme + ".png"
		if c.Screenshot.Path != name {
			return fmt.Errorf("provider screenshot path differs")
		}
		png, err := imageRead(output, name, 4<<20)
		if err != nil || len(png) != c.Screenshot.Bytes || imageHash(png) != c.Screenshot.SHA256 {
			return fmt.Errorf("provider screenshot binding differs")
		}
		config, decodeErr := imagepng.DecodeConfig(bytes.NewReader(png))
		if decodeErr != nil || config.Width < 320 || config.Height < 200 || config.Width > 4096 || config.Height > 2160 {
			return fmt.Errorf("provider screenshot dimensions differ")
		}
		if _, err = imagepng.Decode(bytes.NewReader(png)); err != nil {
			return fmt.Errorf("provider screenshot incomplete")
		}
	}
	return nil
}

// Error cleanup has its own deadline: a failed presentation context cannot
// cancel the native session DELETE before that request reaches the driver.
func (d *browserDriver) closeSession() error {
	old, category := d.ctx, d.category
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.ctx = ctx
	defer func() { d.ctx = old; d.category = category }()
	return d.call("DELETE", "", nil, nil)
}

// Both table row targets are exercised through native row clicks and observed
// local edit forms, rather than inferred from unrelated removal buttons.
func (d *browserDriver) providerTableRoutes(ids []string) error {
	for i, name := range []string{"Fixture Google", "Fixture GitHub"} {
		d.providerStage = "table-navigation-google"
		if i == 1 {
			d.providerStage = "table-navigation-github"
		}
		var element map[string]string
		if err := d.call("POST", "/execute/sync", map[string]any{"script": `const rows=[...document.querySelectorAll('cnsl-idp-table tr')].filter(r=>r.innerText.includes(arguments[0]));return rows.length===1?rows[0]:null;`, "args": []string{name}}, &element); err != nil {
			return err
		}
		id := element["element-6066-11e4-a52e-4f735466cecf"]
		if id == "" || len(id) > 512 {
			return fmt.Errorf("native provider row absent")
		}
		if err := d.call("POST", "/element/"+url.PathEscape(id)+"/click", map[string]any{}, nil); err != nil {
			return err
		}
		route, client := "google", "fictional-google-client"
		if i == 1 {
			route, client = "github", "fictional-github-client"
		}
		expected := "/ui/console/instance/provider/" + route + "/" + ids[i]
		for {
			var ok bool
			if err := d.call("POST", "/execute/sync", map[string]any{"script": `return location.origin==='http://fixture.localhost:8080'&&location.pathname===arguments[0]&&document.querySelector('input[formcontrolname="clientId"]')?.value===arguments[1];`, "args": []string{expected, client}}, &ok); err != nil {
				return err
			}
			if ok {
				break
			}
			select {
			case <-d.ctx.Done():
				return fmt.Errorf("native provider row destination differs")
			case <-time.After(200 * time.Millisecond):
			}
		}
		if err := d.call("POST", "/url", map[string]string{"url": "http://" + serviceAuthority + "/ui/console/instance?id=idp"}, nil); err != nil {
			return err
		}
		if _, err := d.waitPage("provider-console"); err != nil {
			return err
		}
		// Re-read actual table rows before selecting the second one. The parent phase
		// controls the current theme; no row or CSS state is fabricated here.
		for {
			var ready bool
			if err := d.call("POST", "/execute/sync", map[string]any{"script": `return [...document.querySelectorAll('cnsl-idp-table tr')].filter(r=>r.innerText.includes('Fixture Google')||r.innerText.includes('Fixture GitHub')).length===2;`, "args": []any{}}, &ready); err != nil {
				return err
			}
			if ready {
				break
			}
			select {
			case <-d.ctx.Done():
				return fmt.Errorf("native provider rows incomplete")
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}
