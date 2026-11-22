package runtimeapp

import (
	"errors"
	"fmt"
	"testing"
)

// Machine access is optional, paired, scoped to this issuer, and never accepts
// a browser audience, unknown grants, or money/deletion powers at startup.
func TestAgentConfigurationRefusesUnsafeAuthority(t *testing.T) {
	const subject = "ChVidXNpbmVzcy1wb3J0YWwtYWdlbnQ"
	policy := func(issuer, grants string) string {
		return fmt.Sprintf(`[{"issuer":%q,"subject":%q,"grants":%s}]`, issuer, subject, grants)
	}
	for name, extra := range map[string]map[string]string{
		"audience only":    {"PORTAL_AGENT_AUDIENCE": "business-portal-agent"},
		"subjects only":    {"PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `[]`)},
		"browser audience": {"PORTAL_AGENT_AUDIENCE": "business-portal", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `[]`)},
		"other issuer":     {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://other.example.invalid", `[]`)},
		"unknown grant":    {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `["anything.write"]`)},
		"money grant":      {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `["payments.refund"]`)},
		"deletion grant":   {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `["customers.delete"]`)},
		"empty list":       {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": `[]`},
		"extra property":   {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": `[{"issuer":"https://login.example.invalid","subject":"ChVidXNpbmVzcy1wb3J0YWwtYWdlbnQ","operator":true}]`},
		"duplicate grant":  {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", `["invitations.create","invitations.create"]`)},
		"duplicate field":  {"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": `[{"issuer":"https://login.example.invalid","subject":"ChVidXNpbmVzcy1wb3J0YWwtYWdlbnQ","grants":["invitations.create"],"grants":[]}]`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadConfig(settingsEnv(withPortal(extra))); !errors.Is(err, ErrConfig) {
				t.Fatal("unsafe machine configuration was accepted or disclosed details")
			}
		})
	}
	if _, err := ReadConfig(settingsEnv(map[string]string{"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://broker.example.invalid", `[]`)})); !errors.Is(err, ErrConfig) {
		t.Fatal("machine access without a portal was accepted")
	}
	for _, grants := range []string{`[]`, `["invitations.create"]`, `["members.deactivate","members.reactivate"]`} {
		if _, err := ReadConfig(settingsEnv(withPortal(map[string]string{"PORTAL_AGENT_AUDIENCE": "business-portal-agent", "PORTAL_AGENT_SUBJECTS": policy("https://login.example.invalid", grants)}))); err != nil {
			t.Fatal("valid bounded machine configuration was refused")
		}
	}
	requireProjected(t, "PORTAL_AGENT_AUDIENCE", "PORTAL_AGENT_SUBJECTS")
}
