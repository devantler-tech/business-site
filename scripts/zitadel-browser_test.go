package main

import (
	"context"
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
