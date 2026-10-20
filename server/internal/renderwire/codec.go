// Package renderwire encodes the private contact renderer's bounded messages.
// It owns no process, HTTP response, session, authorization or activation.
// The wire format and remaining caller obligations are in server/RENDERER-PROTOCOL.md.
package renderwire

import (
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrMessage carries no private input or renderer output.
var ErrMessage = errors.New("invalid renderer message")

// Response is decoded output, not evidence of authorization or safe HTML.
// Its ID must still match one current pending render in the caller's generation.
type Response struct {
	ID   string
	Body string
}

// ResponseSize validates exactly one four-byte prefix before payload allocation.
func ResponseSize(prefix []byte) (int, error) {
	if len(prefix) != 4 {
		return 0, ErrMessage
	}
	size := binary.BigEndian.Uint32(prefix)
	if size == 0 || size > 64*1024-4 {
		return 0, ErrMessage
	}
	return int(size), nil
}

// DecodeResponse accepts exactly one complete bounded frame, never partial output.
func DecodeResponse(frame []byte) (Response, error) {
	if len(frame) < 4 {
		return Response{}, ErrMessage
	}
	size, err := ResponseSize(frame[:4])
	if err != nil || len(frame) != 4+size {
		return Response{}, ErrMessage
	}
	var wire struct {
		Version *int    `json:"version"`
		ID      *string `json:"id"`
		Body    *string `json:"body"`
	}
	if json.Unmarshal(frame[4:], &wire, json.RejectUnknownMembers(true)) != nil || wire.Version == nil || *wire.Version != 1 ||
		wire.ID == nil || !validID(*wire.ID) || wire.Body == nil || strings.TrimSpace(*wire.Body) == "" {
		// Deliberately discard parser diagnostics and partially decoded values:
		// either can contain private strings supplied by the peer.
		return Response{}, ErrMessage
	}
	return Response{ID: *wire.ID, Body: *wire.Body}, nil
}

// Request is the explicit presentation projection, never a principal or request.
type Request struct {
	ID      string  `json:"id"`
	Contact Contact `json:"contact"`
}

// Contact contains display-only values. Revision remains a decimal string.
// CSRF is the public rendered form nonce, never the cookie or server secret.
type Contact struct {
	Locale       string `json:"locale"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Company      string `json:"company"`
	Revision     string `json:"revision"`
	State        string `json:"state"`
	Verification string `json:"verification"`
	CSRF         string `json:"csrf"`
	Text         Text   `json:"text"`
}

// Text is the closed set of localized plain text, not markup or link destinations.
type Text struct {
	Title            string `json:"title"`
	Intro            string `json:"intro"`
	NameLabel        string `json:"nameLabel"`
	EmailLabel       string `json:"emailLabel"`
	CompanyLabel     string `json:"companyLabel"`
	SaveLabel        string `json:"saveLabel"`
	PendingText      string `json:"pendingText"`
	WorkspaceLabel   string `json:"workspaceLabel"`
	VerifyLabel      string `json:"verifyLabel"`
	VerificationHelp string `json:"verificationHelp"`
	ErrorText        string `json:"errorText"`
	ReloadLabel      string `json:"reloadLabel"`
}

// RequestFrame validates a projection before returning its complete wire frame.
func RequestFrame(r Request) ([]byte, error) {
	if !validID(r.ID) || !validContact(r.Contact) {
		return nil, ErrMessage
	}
	payload, err := json.Marshal(struct {
		Version int     `json:"version"`
		ID      string  `json:"id"`
		Contact Contact `json:"contact"`
	}{Version: 1, ID: r.ID, Contact: r.Contact})
	if err != nil || len(payload) > 8*1024-4 {
		return nil, ErrMessage
	}
	frame := make([]byte, 4, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	return append(frame, payload...), nil
}

// validID admits only the fixed opaque correlation format, not authority.
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validContact validates the closed display projection before encoding.
func validContact(c Contact) bool {
	if c.Locale != "en" && c.Locale != "da" {
		return false
	}
	switch c.State {
	case "normal", "invalid", "stale", "verification":
	default:
		return false
	}
	if (c.State == "normal") != (c.Text.ErrorText == "") {
		return false
	}
	switch c.Verification {
	case "unverified", "confirmable", "verified":
	default:
		return false
	}
	revision, err := strconv.ParseInt(c.Revision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != c.Revision {
		return false
	}
	for _, field := range []struct {
		value    string
		limit    int
		required bool
	}{
		{c.Name, 120, false}, {c.Email, 254, false}, {c.Company, 160, false}, {c.CSRF, 512, true},
		{c.Text.Title, 512, true}, {c.Text.Intro, 512, true}, {c.Text.NameLabel, 512, true},
		{c.Text.EmailLabel, 512, true}, {c.Text.CompanyLabel, 512, true}, {c.Text.SaveLabel, 512, true},
		{c.Text.PendingText, 512, true}, {c.Text.WorkspaceLabel, 512, true}, {c.Text.ReloadLabel, 512, true},
		{c.Text.ErrorText, 512, c.State != "normal"},
		{c.Text.VerifyLabel, 512, c.Verification == "confirmable"},
		{c.Text.VerificationHelp, 512, c.Verification == "confirmable"},
	} {
		if !validText(field.value, field.limit, field.required) {
			return false
		}
	}
	return true
}

// validText bounds display text without changing its content or write policy.
func validText(text string, limit int, required bool) bool {
	if len(text) > utf8.UTFMax*limit || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit || (required && strings.TrimSpace(text) == "") {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
