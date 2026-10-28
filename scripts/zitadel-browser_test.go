package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBrowserRenderedPageAdmission rejects incomplete forms, fonts, navigation
// and resource confinement instead of accepting a merely plausible page URL.
func TestBrowserRenderedPageAdmission(t *testing.T) {
	good := browserPage{URL: "http://fixture.localhost:8080/ui/login/login?authRequest=fictional", Ready: "complete", Text: "Sign in", Username: true,
		Font: "Roboto, sans-serif", FontsReady: true, FaceLoaded: true, SecureContext: true, WebCrypto: true, FailedFonts: []string{}, Resources: []string{"http://fixture.localhost:8080/Roboto.woff2"}}
	if !browserAdmit(good, "login") {
		t.Fatal("rendered native login refused")
	}
	password := good
	password.Username = false
	password.Password = true
	if !browserAdmit(password, "password") {
		t.Fatal("rendered password form refused")
	}
	console := good
	console.URL = "http://fixture.localhost:8080/ui/console/"
	console.Username = false
	console.Console = true
	console.Presented = true
	console.Text = "Welcome to the management console. Users Projects Organizations"
	if !browserAdmit(console, "console") {
		t.Fatal("authenticated console refused")
	}
	for name, change := range map[string]func(*browserPage){
		"external":             func(p *browserPage) { p.URL = "https://external.invalid/ui/login/login" },
		"userinfo":             func(p *browserPage) { p.URL = "http://user@fixture.localhost:8080/ui/login/login" },
		"wrong route":          func(p *browserPage) { p.URL = "http://fixture.localhost:8080/ui/console/" },
		"wrong port":           func(p *browserPage) { p.URL = "http://fixture.localhost:80/ui/login/login" },
		"loading":              func(p *browserPage) { p.Ready = "loading" },
		"empty":                func(p *browserPage) { p.Text = "" },
		"missing form":         func(p *browserPage) { p.Username = false },
		"unadmitted face":      func(p *browserPage) { p.Font = "Ailerons, sans-serif" },
		"no selected face":     func(p *browserPage) { p.FaceLoaded = false },
		"incomplete fonts":     func(p *browserPage) { p.FontsReady = false },
		"failed font":          func(p *browserPage) { p.FailedFonts = []string{"Roboto"} },
		"no resources":         func(p *browserPage) { p.Resources = nil },
		"external resource":    func(p *browserPage) { p.Resources = []string{"https://external.invalid/font.woff2"} },
		"unadmitted resource":  func(p *browserPage) { p.Resources = []string{"http://fixture.localhost:8080/AILERONS.woff"} },
		"insecure context":     func(p *browserPage) { p.SecureContext = false },
		"missing cryptography": func(p *browserPage) { p.WebCrypto = false },
	} {
		t.Run(name, func(t *testing.T) {
			page := good
			change(&page)
			if browserAdmit(page, "login") {
				t.Fatal("incomplete browser observation admitted")
			}
		})
	}
	console.Presented = false
	if browserAdmit(console, "console") {
		t.Fatal("clipped or animating console navigation admitted")
	}
	console.Presented = true
	console.Console = false
	if browserAdmit(console, "console") || browserAdmit(good, "unknown") {
		t.Fatal("empty console or unknown page admitted")
	}
}

// TestBrowserWorkerRefusesUnownedNamespaceWithoutSideEffects verifies that a
// caller outside the admitted service namespace cannot create browser evidence.
func TestBrowserWorkerRefusesUnownedNamespaceWithoutSideEffects(t *testing.T) {
	output := t.TempDir()
	if err := browserWorker(output, "net:[0]", strings.Repeat("a", 64)); err == nil {
		t.Fatal("unowned namespace admitted")
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 0 {
		t.Fatal("refused worker emitted output")
	}
}

// A plausible complete JSON value cannot clear a failed underlying HTTP read.
func TestBrowserDriverRequiresCompleteResponse(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			const body = `{"value":{"ready":true}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/status" || r.Method != "GET" {
					t.Error("wrong WebDriver request")
				}
				if truncated {
					w.Header().Set("Content-Length", fmt.Sprint(len(body)+12))
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "127.0.0.1:9515" {
					return nil, fmt.Errorf("unexpected authority")
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			d := browserDriver{ctx: context.Background(), client: &http.Client{Timeout: time.Second, Transport: transport}}
			var value struct {
				Ready bool `json:"ready"`
			}
			err := d.request("GET", "/status", nil, &value)
			if truncated && (err == nil || value.Ready) {
				t.Fatal("partial HTTP read admitted plausible success")
			}
			if !truncated && (err != nil || !value.Ready) {
				t.Fatalf("complete response refused: %v", err)
			}
		})
	}
}

// TestBrowserDriverRejectsMalformedErrorValue treats any error field as failure,
// including malformed values that otherwise resemble successful driver output.
func TestBrowserDriverRejectsMalformedErrorValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true,"error":false}}`))
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	d := browserDriver{ctx: context.Background(), client: &http.Client{Timeout: time.Second, Transport: transport}}
	var value struct {
		Ready bool `json:"ready"`
	}
	if err := d.request("GET", "/status", nil, &value); err == nil || value.Ready {
		t.Fatal("malformed WebDriver error admitted as success")
	}
}

// TestBrowserOptionalMFARequiresNativeDeferral admits only the native optional
// action on the confined prompt; arbitrary controls cannot defer enrollment.
func TestBrowserOptionalMFARequiresNativeDeferral(t *testing.T) {
	page := browserPage{URL: "http://fixture.localhost:8080/ui/login/password", Ready: "complete", Text: "Set up two-factor authentication", Font: "Roboto, sans-serif", FontsReady: true, FaceLoaded: true, SecureContext: true, WebCrypto: true, FailedFonts: []string{}, Resources: []string{"http://fixture.localhost:8080/font.ttf"}, OptionalMFA: true, MFAAction: "http://fixture.localhost:8080/ui/login/mfa/prompt"}
	if !browserAdmit(page, "optional-mfa") {
		t.Fatal("native optional enrollment form refused")
	}
	for name, change := range map[string]func(*browserPage){
		"mandatory enrollment": func(p *browserPage) { p.OptionalMFA = false },
		"absent action":        func(p *browserPage) { p.MFAAction = "" },
		"external action":      func(p *browserPage) { p.MFAAction = "https://external.invalid/ui/login/mfa/prompt" },
		"other action":         func(p *browserPage) { p.MFAAction = "http://fixture.localhost:8080/ui/login/mfa/verify" },
		"credential action":    func(p *browserPage) { p.MFAAction = "http://user@fixture.localhost:8080/ui/login/mfa/prompt" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := page
			change(&bad)
			if browserAdmit(bad, "optional-mfa") {
				t.Fatal("non-optional or unrelated action admitted")
			}
		})
	}
}

// The driver must use the explicit optional native action once, never clear a
// mandatory prompt or infer that submitting it authenticated the user.
func TestBrowserConsoleWaitHandlesOptionalEnrollment(t *testing.T) {
	for _, scenario := range []string{"optional", "delayed", "repeated", "mandatory", "external", "failed click"} {
		t.Run(scenario, func(t *testing.T) {
			page := browserPage{URL: "http://fixture.localhost:8080/ui/login/password", Ready: "complete", Text: "Set up two-factor authentication", Font: "Roboto, sans-serif", FontsReady: true, FaceLoaded: true, SecureContext: true, WebCrypto: true, FailedFonts: []string{}, Resources: []string{"http://fixture.localhost:8080/font.ttf"}, OptionalMFA: scenario != "mandatory", MFAAction: "http://fixture.localhost:8080/ui/login/mfa/prompt"}
			if scenario == "external" {
				page.MFAAction = "https://external.invalid/ui/login/mfa/prompt"
			}
			clicks := 0
			var clickedAt time.Time
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var value any
				switch r.URL.Path {
				case "/session/fixture/execute/sync":
					current := page
					if scenario == "delayed" && clicks == 1 && time.Since(clickedAt) >= 200*time.Millisecond {
						current.URL = "http://fixture.localhost:8080/ui/console/"
						current.OptionalMFA = false
						current.Console = true
						current.Presented = true
						current.Text = "Welcome to the authenticated management console. Users Projects Organizations"
					}
					value = current
				case "/session/fixture/element":
					value = map[string]string{"element-6066-11e4-a52e-4f735466cecf": "optional-control"}
				case "/session/fixture/element/optional-control/click":
					clicks++
					clickedAt = time.Now()
					if scenario == "failed click" {
						w.WriteHeader(http.StatusInternalServerError)
						value = map[string]string{"error": "unknown error"}
					} else if scenario == "optional" {
						page.URL = "http://fixture.localhost:8080/ui/console/"
						page.OptionalMFA = false
						page.Console = true
						page.Presented = true
						page.Text = "Welcome to the authenticated management console. Users Projects Organizations"
					}
				default:
					t.Error("unexpected driver operation")
					w.WriteHeader(http.StatusNotFound)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"value": value}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			d := browserDriver{ctx: ctx, client: &http.Client{Timeout: time.Second, Transport: transport}, session: "fixture"}
			observed, err := d.waitPage("console")
			if scenario == "optional" || scenario == "delayed" {
				if err != nil || !observed.Console || !d.mfaDeferred || clicks != 1 {
					t.Fatalf("optional enrollment journey failed: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("incomplete authentication admitted")
				}
				wantClicks := 0
				if scenario == "repeated" || scenario == "failed click" {
					wantClicks = 1
				}
				if clicks != wantClicks {
					t.Fatal("unrelated or repeated prompt submitted")
				}
			}
		})
	}
}

// TestBrowserDriverAllowsBoundedSessionStartup exercises an actual delayed HTTP
// response beyond eight seconds while preserving the finite request deadline.
func TestBrowserDriverAllowsBoundedSessionStartup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(8500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"value":{"sessionId":"00000000000000000000000000000000","capabilities":{"browserVersion":"fixture"}}}`))
	}))
	defer server.Close()
	client := browserHTTPClient()
	transport := client.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	d := browserDriver{ctx: ctx, client: client}
	var session struct {
		ID string `json:"sessionId"`
	}
	if err := d.request("POST", "/session", map[string]any{}, &session); err != nil || session.ID == "" {
		t.Fatalf("bounded browser startup refused: %v", err)
	}
}

// One successful URL or correct glyph cannot clear an incomplete native surface.
func TestBrowserProviderObservationAdmission(t *testing.T) {
	good := browserProviderPage{URL: "http://fixture.localhost:8080/ui/console/instance?id=idp", Kind: "picker", Theme: "light", Count: 9, Glyphs: true, Labels: true, Targets: true, ThemeReady: true, Resources: true, Keyboard: true}
	if !browserProviderAdmit(good, "picker", "light") {
		t.Fatal("complete picker refused")
	}
	for name, change := range map[string]func(*browserProviderPage){
		"external":          func(p *browserProviderPage) { p.URL = "https://outside.invalid/ui/console/instance?id=idp" },
		"wrong route":       func(p *browserProviderPage) { p.URL = "http://fixture.localhost:8080/ui/console/users" },
		"missing icon":      func(p *browserProviderPage) { p.Count = 8 },
		"wrong theme":       func(p *browserProviderPage) { p.Theme = "dark" },
		"missing glyph":     func(p *browserProviderPage) { p.Glyphs = false },
		"missing label":     func(p *browserProviderPage) { p.Labels = false },
		"changed target":    func(p *browserProviderPage) { p.Targets = false },
		"missing theme":     func(p *browserProviderPage) { p.ThemeReady = false },
		"external resource": func(p *browserProviderPage) { p.Resources = false },
		"unreachable link":  func(p *browserProviderPage) { p.Keyboard = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			change(&bad)
			if browserProviderAdmit(bad, "picker", "light") {
				t.Fatal("incomplete provider observation admitted")
			}
		})
	}
	for _, kind := range []string{"table", "editor-google", "login", "registration"} {
		page := good
		page.Kind = kind
		page.Count = 2
		switch kind {
		case "editor-google":
			page.Count = 1
			page.URL = "http://fixture.localhost:8080/ui/console/instance/provider/google/create"
		case "login":
			page.URL = "http://fixture.localhost:8080/ui/login/login?authRequest=fictional"
		case "registration":
			page.URL = "http://fixture.localhost:8080/ui/login/register/option?authRequest=fictional"
		}
		if !browserProviderAdmit(page, kind, "light") {
			t.Fatalf("complete %s refused", kind)
		}
		page.URL = "http://fixture.localhost:8080/incorrect"
		if browserProviderAdmit(page, kind, "light") {
			t.Fatalf("wrong %s route admitted", kind)
		}
	}
}

func TestBrowserProviderReceiptBindsCompleteCaseSet(t *testing.T) {
	output := t.TempDir()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 320, 200))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	cases := []browserProviderCase{}
	for _, name := range []string{"picker/light", "table/light", "editor-google/light", "picker/dark", "table/dark", "editor-google/dark", "login/light", "registration/light", "login/dark", "registration/dark"} {
		parts := strings.Split(name, "/")
		kind, theme := parts[0], parts[1]
		count := 2
		if kind == "picker" {
			count = 9
		}
		if kind == "editor-google" {
			count = 1
		}
		filename := "provider-" + kind + "-" + theme + ".png"
		if err := os.WriteFile(filepath.Join(output, filename), data, 0600); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, browserProviderCase{kind, theme, count, true, true, true, true, true, true, browserScreen{filename, len(data), imageHash(data)}})
	}
	if err := browserProviderReceipt(output, cases); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]browserProviderCase) []browserProviderCase{
		"missing screen":   func(c []browserProviderCase) []browserProviderCase { return c[:9] },
		"duplicate screen": func(c []browserProviderCase) []browserProviderCase { c[1] = c[0]; return c },
		"missing glyph":    func(c []browserProviderCase) []browserProviderCase { c[7].Glyphs = false; return c },
		"unsafe path":      func(c []browserProviderCase) []browserProviderCase { c[7].Screenshot.Path = "../outside.png"; return c },
		"changed digest": func(c []browserProviderCase) []browserProviderCase {
			c[7].Screenshot.SHA256 = strings.Repeat("0", 64)
			return c
		},
		"changed length": func(c []browserProviderCase) []browserProviderCase { c[7].Screenshot.Bytes++; return c },
	} {
		t.Run(name, func(t *testing.T) {
			bad := append([]browserProviderCase{}, cases...)
			if browserProviderReceipt(output, mutate(bad)) == nil {
				t.Fatal("incomplete case set admitted")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(output, cases[0].Screenshot.Path), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if browserProviderReceipt(output, cases) == nil {
		t.Fatal("changed screenshot admitted")
	}
}

func TestBrowserCleanupSurvivesExpiredObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	transport := browserRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if req.Method != "DELETE" || req.URL.Path != "/session/fixture" {
			t.Fatal("wrong cleanup target")
		}
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":null}`)), Header: make(http.Header)}, nil
	})
	d := browserDriver{ctx: ctx, client: &http.Client{Transport: transport}, session: "fixture", category: "observation"}
	if err := d.closeSession(); err != nil || !called || d.ctx != ctx || d.category != "observation" {
		t.Fatal("expired observation cancelled independent cleanup", err)
	}
}

type browserRoundTrip func(*http.Request) (*http.Response, error)

func (f browserRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
