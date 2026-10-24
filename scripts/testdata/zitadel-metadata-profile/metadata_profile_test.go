package id

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
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
