package id

import (
	"bytes"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// The selected distribution preserves the raw-response path, even for data
// that is not JSON; interpreting it would change the machine identifier.
func TestCustomerMetadataProfilePreservesRawBytes(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("42\r\n"), []byte("not JSON"), {0, 255, 1}} {
		got, err := extractMetadataResponse(nil, raw)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("raw response changed: got %q, %v; want %q", got, err, raw)
		}
	}
}

// Reject the configuration before selecting another method or contacting the
// metadata service. A private-IP/hostname success or HTTP failure must not hide it.
func TestCustomerMetadataProfileRejectsBeforeMachineSelection(t *testing.T) {
	before := GeneratorConfig
	t.Cleanup(func() { GeneratorConfig = before })
	for _, selector := range []string{"$.id", "", "$", "$..id", "invalid"} {
		for _, scenario := range []struct {
			name                   string
			private, host, webhook bool
		}{
			{"private IP and hostname available", true, true, true},
			{"failing metadata endpoint", false, false, true},
			{"webhook disabled", false, true, false},
		} {
			t.Run(selector+"/"+scenario.name, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				defer server.Close()
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("machine selection panicked instead of returning the unsupported-profile error: %v", recovered)
					}
					if got := requests.Load(); got != 0 {
						t.Errorf("refused selector reached metadata service %d times", got)
					}
				}()
				GeneratorConfig = &Config{Identification: Identification{
					PrivateIp: PrivateIp{Enabled: scenario.private},
					Hostname:  Hostname{Enabled: scenario.host},
					Webhook:   Webhook{Enabled: scenario.webhook, Url: server.URL, JPath: &selector},
				}}
				got, err := machineID()
				if got != 0 || err == nil || !strings.Contains(err.Error(), "unsupported by this source profile") {
					t.Fatalf("machine selection accepted refused selector: got %d, %v", got, err)
				}
			})
		}
	}
}

// A nil selector retains the native hostname-before-webhook order and the
// exact raw-response identifier. The literal is FNV-1("42\r\n") modulo 65536.
func TestCustomerMetadataProfilePreservesMachineSelection(t *testing.T) {
	before := GeneratorConfig
	t.Cleanup(func() { GeneratorConfig = before })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("42\r\n"))
	}))
	defer server.Close()
	GeneratorConfig = &Config{Identification: Identification{Webhook: Webhook{Enabled: true, Url: server.URL}}}
	got, err := machineID()
	if got != 21974 || err != nil || requests.Load() != 1 {
		t.Fatalf("raw webhook identifier changed: got %d, %v; requests %d", got, err, requests.Load())
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	// The independent expectation uses the OS hostname, not the production helper.
	hash := fnv.New32()
	_, _ = hash.Write([]byte(host))
	wantHostname := uint16(hash.Sum32())
	GeneratorConfig.Identification.Hostname.Enabled = true
	got, err = machineID()
	if got != wantHostname || err != nil || requests.Load() != 1 {
		t.Fatalf("hostname no longer precedes webhook: got %d, %v; want %d, requests %d", got, err, wantHostname, requests.Load())
	}
}

// A successfully read HTTP response must not hide the selector refusal and
// become a valid hashed machine identifier.
func TestCustomerMetadataProfilePropagatesWebhookRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer server.Close()
	before := GeneratorConfig
	t.Cleanup(func() { GeneratorConfig = before })
	selector := "$.id"
	GeneratorConfig = &Config{Identification: Identification{Webhook: Webhook{Enabled: true, Url: server.URL, JPath: &selector}}}
	got, err := metadataWebhookID()
	if got != 0 || err == nil || !strings.Contains(err.Error(), "unsupported by this source profile") {
		t.Fatalf("webhook accepted refused selector: got %d, %v", got, err)
	}
}

// The optional selector must fail explicitly, never execute an unadmitted
// parser, silently return the raw bytes, or return a different machine ID.
func TestCustomerMetadataProfileRefusesEveryConfiguredSelector(t *testing.T) {
	for _, selector := range []string{"$.id", "", "$", "$..id", "invalid"} {
		t.Run(selector, func(t *testing.T) {
			got, err := extractMetadataResponse(&selector, []byte(`{"id":7}`))
			if len(got) != 0 || err == nil || !strings.Contains(err.Error(), "unsupported by this source profile") {
				t.Fatalf("selector was not explicitly refused: got %q, %v", got, err)
			}
		})
	}
}
