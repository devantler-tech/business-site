package customeridentity

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// TestDexSubjectMatchesIssuedTokens uses subjects Dex 2.46.0 publishes from real
// issued tokens, plus an independently encoded ZITADEL-shaped subject.
func TestDexSubjectMatchesIssuedTokens(t *testing.T) {
	for _, tc := range []struct{ user, connector, want string }{
		// examples/example-app/server/subject_test.go at Dex v2.46.0.
		{"0-385-28089-0", "mock", "Cg0wLTM4NS0yODA4OS0wEgRtb2Nr"},
		{"08a8684b-db88-4b73-90a9-3cd1661f5466", "local", "CiQwOGE4Njg0Yi1kYjg4LTRiNzMtOTBhOS0zY2QxNjYxZjU0NjYSBWxvY2Fs"},
		// A numeric ZITADEL user ID through the customer connector.
		{"345678901234567890", ZitadelConnectorID, "ChIzNDU2Nzg5MDEyMzQ1Njc4OTASEGN1c3RvbWVyLXppdGFkZWw"},
	} {
		if got := DexSubject(tc.user, tc.connector); got != tc.want {
			t.Errorf("DexSubject(%q, %q) = %q, want %q", tc.user, tc.connector, got, tc.want)
		}
	}
	// A 130-byte ID needs a two-byte length varint: 130 = 0x82 0x01.
	long := strings.Repeat("u", 130)
	want := append(append([]byte{0x0a, 0x82, 0x01}, long...), append([]byte{0x12, 0x10}, ZitadelConnectorID...)...)
	got, err := base64.RawURLEncoding.DecodeString(DexSubject(long, ZitadelConnectorID))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("a long user ID was not encoded as Dex's length-delimited field")
	}
	if ZitadelConnectorID != "customer-zitadel" {
		t.Fatal("the adopted customer connector ID changed without an identity migration")
	}
}
