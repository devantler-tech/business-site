package renderwire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const fixtureID = "0123456789abcdef0123456789abcdef"

// fixtureRequest supplies fictional display values with an exact large revision.
func fixtureRequest() Request {
	return Request{ID: fixtureID, Contact: Contact{
		Locale: "en", Name: "Fictional Client <&>", Email: "client@example.com", Company: "Example",
		Revision: "9223372036854775807", State: "normal", Verification: "unverified", CSRF: "fictional-form-nonce",
		Text: Text{Title: "Contact details", Intro: "Your private profile", NameLabel: "Your name", EmailLabel: "Contact email",
			CompanyLabel: "Company", SaveLabel: "Save", PendingText: "Email is not verified", WorkspaceLabel: "Workspace",
			VerifyLabel: "Verify", VerificationHelp: "Confirm the saved address", ReloadLabel: "Reload"},
	}}
}

// TestRequestFrameLimits catches a projection escaping the aggregate byte cap,
// including valid multi-byte text whose individual fields fit their limits.
func TestRequestFrameLimits(t *testing.T) {
	r := fixtureRequest()
	r.Contact.State = "invalid"
	fill := strings.Repeat("x", 512)
	r.Contact.Text = Text{Title: fill, Intro: strings.Repeat("😀", 512), NameLabel: fill, EmailLabel: fill,
		CompanyLabel: fill, SaveLabel: fill, PendingText: fill, WorkspaceLabel: fill, VerifyLabel: fill,
		VerificationHelp: fill, ErrorText: fill, ReloadLabel: fill}
	r.Contact.CSRF = fill
	r.Contact.Name = strings.Repeat("x", 120)
	r.Contact.Email = strings.Repeat("x", 254)
	r.Contact.Company = strings.Repeat("x", 160)
	frame, err := RequestFrame(r)
	if !errors.Is(err, ErrMessage) || len(frame) != 0 {
		t.Fatal("oversized wire projection was returned")
	}

	r = fixtureRequest()
	r.Contact.Name = string([]byte{0xff})
	if frame, err := RequestFrame(r); !errors.Is(err, ErrMessage) || len(frame) != 0 {
		t.Fatal("invalid UTF-8 became a renderer input")
	}
}

// TestRequestFrameProjection catches omitted/expanded projection fields, lost
// revision precision and an incorrect prefix in the actual encoded message.
func TestRequestFrameProjection(t *testing.T) {
	frame, err := RequestFrame(fixtureRequest())
	if err != nil {
		t.Fatal("valid projection rejected", err)
	}
	if len(frame) < 4 || int(binary.BigEndian.Uint32(frame[:4])) != len(frame)-4 {
		t.Fatal("wrong length prefix")
	}
	var wire struct {
		Version int                        `json:"version"`
		ID      string                     `json:"id"`
		Contact map[string]json.RawMessage `json:"contact"`
	}
	if err := json.Unmarshal(frame[4:], &wire); err != nil {
		t.Fatal("encoded frame is not JSON")
	}
	if wire.Version != 1 || wire.ID != fixtureID || len(wire.Contact) != 9 {
		t.Fatal("wrong projection envelope")
	}
	for _, key := range []string{"locale", "name", "email", "company", "revision", "state", "verification", "csrf", "text"} {
		if _, exists := wire.Contact[key]; !exists {
			t.Fatalf("missing projection field %s", key)
		}
	}
	var revision, name string
	if json.Unmarshal(wire.Contact["revision"], &revision) != nil || revision != "9223372036854775807" {
		t.Fatal("revision lost its exact decimal string")
	}
	if json.Unmarshal(wire.Contact["name"], &name) != nil || name != "Fictional Client <&>" {
		t.Fatal("display text was changed")
	}
}

// TestRequestProjectionRefusals catches malformed display values reaching a
// renderer; recovery text is bounded, not revalidated as a new profile write.
func TestRequestProjectionRefusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Request)
	}{
		{"missing id", func(r *Request) { r.ID = "" }},
		{"long id", func(r *Request) { r.ID += "0" }},
		{"uppercase id", func(r *Request) { r.ID = strings.ToUpper(fixtureID) }},
		{"non-hexadecimal id", func(r *Request) { r.ID = strings.Repeat("z", 32) }},
		{"unknown locale", func(r *Request) { r.Contact.Locale = "de" }},
		{"missing locale", func(r *Request) { r.Contact.Locale = "" }},
		{"unknown state", func(r *Request) { r.Contact.State = "approved" }},
		{"missing state", func(r *Request) { r.Contact.State = "" }},
		{"unknown verification", func(r *Request) { r.Contact.Verification = "authorized" }},
		{"missing verification", func(r *Request) { r.Contact.Verification = "" }},
		{"missing revision", func(r *Request) { r.Contact.Revision = "" }},
		{"negative revision", func(r *Request) { r.Contact.Revision = "-1" }},
		{"overflow revision", func(r *Request) { r.Contact.Revision = "9223372036854775808" }},
		{"fractional revision", func(r *Request) { r.Contact.Revision = "1.5" }},
		{"exponent revision", func(r *Request) { r.Contact.Revision = "1e1" }},
		{"non-canonical revision", func(r *Request) { r.Contact.Revision = "01" }},
		{"signed revision", func(r *Request) { r.Contact.Revision = "+1" }},
		{"long name", func(r *Request) { r.Contact.Name = strings.Repeat("x", 121) }},
		{"long multi-byte name", func(r *Request) { r.Contact.Name = strings.Repeat("😀", 121) }},
		{"long email", func(r *Request) { r.Contact.Email = strings.Repeat("x", 255) }},
		{"long company", func(r *Request) { r.Contact.Company = strings.Repeat("x", 161) }},
		{"control character", func(r *Request) { r.Contact.Company = "private-value\x00" }},
		{"missing nonce", func(r *Request) { r.Contact.CSRF = "" }},
		{"long nonce", func(r *Request) { r.Contact.CSRF = strings.Repeat("x", 513) }},
		{"missing label", func(r *Request) { r.Contact.Text.SaveLabel = " " }},
		{"long copy", func(r *Request) { r.Contact.Text.Intro = strings.Repeat("x", 513) }},
		{"invalid copy encoding", func(r *Request) { r.Contact.Text.Intro = string([]byte{0xff}) }},
		{"control in copy", func(r *Request) { r.Contact.Text.Intro = "private-value\n" }},
		{"error without state", func(r *Request) { r.Contact.Text.ErrorText = "Please review" }},
		{"state without error", func(r *Request) { r.Contact.State = "invalid" }},
		{"missing verification label", func(r *Request) { r.Contact.Verification = "confirmable"; r.Contact.Text.VerifyLabel = "" }},
		{"missing verification help", func(r *Request) { r.Contact.Verification = "confirmable"; r.Contact.Text.VerificationHelp = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRequest()
			tc.edit(&r)
			frame, err := RequestFrame(r)
			if err != ErrMessage || len(frame) != 0 {
				t.Fatal("invalid projection admitted or private diagnostic returned")
			}
		})
	}
}

// TestRequestDisplayStates preserves both locales, all recovery forms, an
// initially empty profile and high Unicode values without claiming authority.
func TestRequestDisplayStates(t *testing.T) {
	for _, locale := range []string{"en", "da"} {
		for _, state := range []string{"normal", "invalid", "stale", "verification"} {
			for _, verification := range []string{"unverified", "confirmable", "verified"} {
				t.Run(locale+"/"+state+"/"+verification, func(t *testing.T) {
					r := fixtureRequest()
					r.Contact.Locale, r.Contact.State, r.Contact.Verification = locale, state, verification
					r.Contact.Name, r.Contact.Email, r.Contact.Company, r.Contact.Revision = "", "", "", "0"
					if state != "normal" {
						r.Contact.Text.ErrorText = "Please review the details"
					}
					r.Contact.Text.Title = "Contact 😀"
					if _, err := RequestFrame(r); err != nil {
						t.Fatal("valid display projection rejected")
					}
				})
			}
		}
	}
}

// responseFrame is an independently constructed peer fixture, not a production
// encoder shared with the decoder under test.
func responseFrame(payload string) []byte {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	return append(header, payload...)
}

// TestResponseEnvelope catches lost output, altered correlation and accidental
// JavaScript-style coercion of the peer's envelope.
func TestResponseEnvelope(t *testing.T) {
	frame := responseFrame(`{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"<main>Fictional 😀</main>"}`)
	size, err := ResponseSize(frame[:4])
	if err != nil || size != len(frame)-4 {
		t.Fatal("valid prefix rejected or altered")
	}
	response, err := DecodeResponse(frame)
	if err != nil || response.ID != fixtureID || response.Body != "<main>Fictional 😀</main>" {
		t.Fatal("valid response changed or rejected")
	}
}

// TestResponseSize rejects an untrusted allocation size before any body read.
func TestResponseSize(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header []byte
		want   int
	}{
		{"one byte", []byte{0, 0, 0, 1}, 1},
		{"network order", []byte{0, 0, 1, 0}, 256},
		{"at limit including prefix", []byte{0, 0, 255, 252}, 65532},
		{"over limit including prefix", []byte{0, 0, 255, 253}, 0},
		{"payload alone at limit", []byte{0, 1, 0, 0}, 0},
		{"maximum integer", []byte{255, 255, 255, 255}, 0},
		{"empty payload", []byte{0, 0, 0, 0}, 0},
		{"missing prefix", nil, 0},
		{"partial prefix", []byte{0, 1, 0}, 0},
		{"prefix plus data", []byte{0, 0, 0, 1, 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			size, err := ResponseSize(tc.header)
			if tc.want == 0 {
				if size != 0 || err != ErrMessage {
					t.Fatal("invalid allocation size admitted")
				}
			} else if err != nil || size != tc.want {
				t.Fatal("valid allocation size changed or rejected")
			}
		})
	}
}

// TestResponseRefusals catches permissive parsers and partial-private-output
// returns, including an unknown member following an otherwise valid body.
func TestResponseRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
	}{
		{"missing version", `{"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"missing id", `{"version":1,"body":"private-value"}`},
		{"missing body", `{"version":1,"id":"0123456789abcdef0123456789abcdef"}`},
		{"null version", `{"version":null,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"null id", `{"version":1,"id":null,"body":"private-value"}`},
		{"null body", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":null}`},
		{"unsupported version", `{"version":2,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"string version", `{"version":"1","id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"fractional version", `{"version":1.5,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"numeric id", `{"version":1,"id":1,"body":"private-value"}`},
		{"unknown member", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value","extra":true}`},
		{"authority member", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value","clientId":"foreign"}`},
		{"case alias", `{"Version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"case alias beside field", `{"version":1,"Version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"short id", `{"version":1,"id":"0123456789abcdef","body":"private-value"}`},
		{"uppercase id", `{"version":1,"id":"0123456789ABCDEF0123456789ABCDEF","body":"private-value"}`},
		{"non-hexadecimal id", `{"version":1,"id":"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz","body":"private-value"}`},
		{"duplicate version", `{"version":1,"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"duplicate body", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value","body":"replacement"}`},
		{"duplicate escaped id", `{"version":1,"id":"0123456789abcdef0123456789abcdef","\u0069d":"0123456789abcdef0123456789abcdef","body":"private-value"}`},
		{"empty body", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":""}`},
		{"whitespace body", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":" \n\t"}`},
		{"object body", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":{"private":"value"}}`},
		{"invalid escape", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"\x20"}`},
		{"unpaired high surrogate", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"\ud800"}`},
		{"unpaired low surrogate", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"\udfff"}`},
		{"trailing value", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value"} {}`},
		{"truncated JSON", `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"private-value"`},
		{"null envelope", `null`},
		{"array envelope", `[]`},
		{"invalid UTF-8", "{\"version\":1,\"id\":\"0123456789abcdef0123456789abcdef\",\"body\":\"\xff\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := DecodeResponse(responseFrame(tc.payload))
			if err != ErrMessage || response != (Response{}) {
				t.Fatal("invalid peer input admitted or private data returned")
			}
		})
	}
}

// TestCompleteFrameOnly ensures no prefix/JSON fragment, appended byte or second
// frame can produce a partially decoded result.
func TestCompleteFrameOnly(t *testing.T) {
	frame := responseFrame(`{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"<main>😀</main>"}`)
	for cut := range len(frame) {
		response, err := DecodeResponse(frame[:cut])
		if err != ErrMessage || response != (Response{}) {
			t.Fatalf("incomplete frame accepted at byte %d", cut)
		}
	}
	for _, tail := range [][]byte{{'\n'}, frame} {
		joined := append(append([]byte{}, frame...), tail...)
		if response, err := DecodeResponse(joined); err != ErrMessage || response != (Response{}) {
			t.Fatal("trailing frame data admitted")
		}
	}
}

// TestResponseByteCap exercises the entire maximum-sized valid envelope, not
// just a matching prefix. JSON whitespace remains inside the declared payload.
func TestResponseByteCap(t *testing.T) {
	payload := `{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"<p>value</p>"}`
	payload += strings.Repeat(" ", 65532-len(payload))
	response, err := DecodeResponse(responseFrame(payload))
	if err != nil || response.Body != "<p>value</p>" || response.ID != fixtureID {
		t.Fatal("maximum valid response rejected")
	}
	if response, err := DecodeResponse(responseFrame(payload + " ")); err != ErrMessage || response != (Response{}) {
		t.Fatal("oversized response admitted")
	}
}

// TestRequestByteCap proves the exact aggregate boundary with individually
// valid fields; one added ASCII byte must be refused, not truncated.
func TestRequestByteCap(t *testing.T) {
	r := fixtureRequest()
	fill := strings.Repeat("x", 512)
	r.Contact.State = "invalid"
	r.Contact.Text = Text{Title: strings.Repeat("😀", 320), Intro: "i", NameLabel: fill, EmailLabel: fill,
		CompanyLabel: fill, SaveLabel: fill, PendingText: fill, WorkspaceLabel: fill, VerifyLabel: fill,
		VerificationHelp: fill, ErrorText: fill, ReloadLabel: fill}
	r.Contact.Name, r.Contact.Email, r.Contact.Company = strings.Repeat("x", 120), strings.Repeat("x", 254), strings.Repeat("x", 160)
	r.Contact.CSRF = fill
	// Independent standard JSON encoding determines the fixture's padding. No
	// private encoder/validator under test supplies the expected boundary.
	payload, err := json.Marshal(map[string]any{"version": 1, "id": fixtureID, "contact": r.Contact})
	if err != nil {
		t.Fatal("fixture encoding failed")
	}
	padding := 8188 - len(payload)
	if padding < 0 || padding >= 511 {
		t.Fatal("fixture cannot reach the byte cap within the text limit")
	}
	r.Contact.Text.Intro += strings.Repeat("i", padding)
	frame, err := RequestFrame(r)
	if err != nil || len(frame) != 8192 || binary.BigEndian.Uint32(frame[:4]) != 8188 {
		t.Fatal("maximum valid request changed or rejected")
	}
	r.Contact.Text.Intro += "i"
	if frame, err := RequestFrame(r); err != ErrMessage || len(frame) != 0 {
		t.Fatal("one-byte request overflow admitted")
	}
}

// FuzzDecodeResponse rejects a partial result on every failed or incomplete
// observation and retains the total byte budget on every admitted result.
func FuzzDecodeResponse(f *testing.F) {
	f.Add(responseFrame(`{"version":1,"id":"0123456789abcdef0123456789abcdef","body":"<p>value</p>"}`))
	f.Add([]byte{255, 255, 255, 255})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, frame []byte) {
		response, err := DecodeResponse(frame)
		if err != nil {
			if err != ErrMessage || response != (Response{}) {
				t.Fatal("failed observation returned private output or diagnostics")
			}
			return
		}
		if len(frame) > 65536 || len(frame) < 4 || strings.TrimSpace(response.Body) == "" || len(response.ID) != 32 {
			t.Fatal("admitted response violates the wire budget or required fields")
		}
	})
}
