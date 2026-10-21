package contacttrialhost

import (
	"strings"
	"testing"
)

const setupJSON = `{"mode":"go",
"database":"postgres://portal_runtime:fixture-only@127.0.0.1:55444/portal_trial_0123456789abcdef0123456789abcdef?sslmode=disable",
"issuer":"https://127.0.0.1:43111","callback":"https://127.0.0.1:43112/portal/callback","certificate":"certificate","key":"key","trust":"trust"}`

func TestSetupIsClosedAndDisposable(t *testing.T) {
	for _, mode := range []string{"go", "closed"} {
		input := strings.Replace(setupJSON, `"mode":"go"`, `"mode":"`+mode+`"`, 1)
		if _, err := readSetup(strings.NewReader(input)); err != nil {
			t.Errorf("valid explicit %s setup rejected", mode)
		}
	}
	for _, tc := range []struct{ name, input string }{
		{"empty", ""}, {"null", "null"}, {"array", "[]"},
		{"unknown field", strings.Replace(setupJSON, `"mode"`, `"unknown"`, 1)},
		{"duplicate field", strings.Replace(setupJSON, `"mode":"go"`, `"mode":"closed","mode":"go"`, 1)},
		{"non-string", strings.Replace(setupJSON, `"mode":"go"`, `"mode":true`, 1)},
		{"missing mode", strings.Replace(setupJSON, `"mode":"go",`, "", 1)},
		{"unknown mode", strings.Replace(setupJSON, `"go"`, `"public"`, 1)},
		{"trailing input", setupJSON + `{}`},
		{"oversized", strings.Repeat(" ", 65537) + setupJSON},
		{"remote database", strings.Replace(setupJSON, "127.0.0.1:55444", "database.example.invalid:55444", 1)},
		{"ordinary database", strings.Replace(setupJSON, "portal_trial_0123456789abcdef0123456789abcdef", "portal_identity_test", 1)},
		{"broad role", strings.Replace(setupJSON, "portal_runtime", "portal_fixture_owner", 1)},
		{"database startup options", strings.Replace(setupJSON, "sslmode=disable", "sslmode=disable&options=-c+role=portal_runtime", 1)},
		{"remote issuer", strings.Replace(setupJSON, "127.0.0.1:43111", "issuer.example.invalid", 1)},
		{"non-TLS issuer", strings.Replace(setupJSON, "https://127.0.0.1:43111", "http://127.0.0.1:43111", 1)},
		{"issuer path", strings.Replace(setupJSON, "127.0.0.1:43111", "127.0.0.1:43111/issuer", 1)},
		{"callback query", strings.Replace(setupJSON, "/portal/callback", "/portal/callback?extra=1", 1)},
		{"callback path", strings.Replace(setupJSON, "/portal/callback", "/callback", 1)},
		{"missing key", strings.Replace(setupJSON, `"key":"key"`, `"key":""`, 1)},
		{"implicit candidate", strings.TrimSuffix(setupJSON, "}") + `,"node":"/fixture/node"}`},
		{"missing candidate", strings.Replace(setupJSON, `"mode":"go"`, `"mode":"svelte"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readSetup(strings.NewReader(tc.input)); err == nil {
				t.Fatal("unsafe or ambiguous setup accepted")
			}
		})
	}
}

// Retired candidate inputs must fail, not silently enter a retained Go mode.
func TestSetupRejectsRetiredCandidateInputs(t *testing.T) {
	for _, fields := range []string{
		`,"node":"","worker":""`,
		`,"node":"/fixture/node","worker":"/fixture/worker.mjs"`,
		`,"node":"/fixture/node"`,
		`,"worker":"/fixture/worker.mjs"`,
	} {
		for _, mode := range []string{"go", "closed", "svelte"} {
			t.Run(mode+fields, func(t *testing.T) {
				input := strings.Replace(setupJSON, `"mode":"go"`, `"mode":"`+mode+`"`, 1)
				if _, err := readSetup(strings.NewReader(strings.TrimSuffix(input, "}") + fields + "}")); err == nil {
					t.Fatal("retired candidate setup accepted")
				}
			})
		}
	}
}
