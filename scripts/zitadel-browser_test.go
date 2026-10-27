package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

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
	console.Console = false
	if browserAdmit(console, "console") || browserAdmit(good, "unknown") {
		t.Fatal("empty console or unknown page admitted")
	}
}

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
