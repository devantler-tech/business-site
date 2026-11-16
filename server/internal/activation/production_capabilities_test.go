package activation

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
)

var capabilityCases = []struct{ key, setting string }{
	{"portal-contact-verification", "PORTAL_CONTACT_VERIFICATION_FLAG"},
	{"portal-requests", "PORTAL_REQUESTS_FLAG"},
	{"portal-triage", "PORTAL_TRIAGE_FLAG"},
	{"portal-proposals", "PORTAL_PROPOSALS_FLAG"},
	{"portal-proposal-sharing", "PORTAL_PROPOSAL_SHARING_FLAG"},
	{"portal-proposal-acceptance", "PORTAL_PROPOSAL_ACCEPTANCE_FLAG"},
	{"portal-invoice-billing", "PORTAL_INVOICE_BILLING_FLAG"},
	{"portal-card-billing", "PORTAL_CARD_BILLING_FLAG"},
}

const off = "{\"defaultVariant\":\"off\",\"variants\":[{\"name\":\"off\",\"value\":false}]}"

func clearCapabilitySettings(t *testing.T) {
	t.Helper()
	for _, capability := range capabilityCases {
		t.Setenv(capability.setting, "")
		if err := os.Unsetenv(capability.setting); err != nil {
			t.Fatal(err)
		}
	}
}

// Removing a mapping or coupling it to another setting must fail this real SDK journey.
func TestProductionCapabilitiesRequireTheirOwnSettings(t *testing.T) {
	for _, selected := range capabilityCases {
		t.Run(selected.key, func(t *testing.T) {
			clearCapabilitySettings(t)
			t.Setenv(selected.setting, on)
			client, _ := productionClient(context.Background(), t)
			for _, capability := range capabilityCases {
				value, err := client.BooleanValue(context.Background(), capability.key, true, openfeature.NewEvaluationContext("", nil))
				if err != nil || value != (capability.key == selected.key) {
					t.Fatalf("%s did not follow its own explicit setting: value=%t error=%v", capability.key, value, err)
				}
			}
			for _, invalid := range []string{"", off, "true", "{invalid", "null", "{\"defaultVariant\":\"on\",\"variants\":[{\"name\":\"on\",\"value\":\"true\"}]}", strings.Repeat("x", 16385)} {
				t.Setenv(selected.setting, invalid)
				value, _ := client.BooleanValue(context.Background(), selected.key, false, openfeature.NewEvaluationContext("", nil))
				if value {
					t.Fatal("absent, false, malformed, wrong-type or oversized data retained capability authority")
				}
			}
			t.Setenv(selected.setting, on)
			if value, err := client.BooleanValue(context.Background(), selected.key, false, openfeature.NewEvaluationContext("", nil)); !value || err != nil {
				t.Fatal("restored explicit setting was not exercised")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if value, err := client.BooleanValue(ctx, selected.key, false, openfeature.NewEvaluationContext("", nil)); value || err == nil {
				t.Fatal("cancelled evaluation retained capability authority")
			}
		})
	}
}

// Skipping optional-key validation must not admit a malformed production profile.
func TestProductionCapabilitiesRejectMalformedStartup(t *testing.T) {
	for _, capability := range capabilityCases {
		t.Run(capability.key, func(t *testing.T) {
			clearCapabilitySettings(t)
			t.Setenv("PORTAL_IDENTITY_PROFILE", "production")
			t.Setenv("PORTAL_IDENTITY_LEASE_UNTIL", "")
			t.Setenv("PORTAL_IDENTITY_FLAG", on)
			for _, invalid := range []string{"true", "{private-invalid-value", "null", "{\"defaultVariant\":\"on\",\"variants\":[{\"name\":\"on\",\"value\":1}]}", strings.Repeat("x", 16385)} {
				t.Setenv(capability.setting, invalid)
				if _, err := NewProduction(context.Background()); err == nil || err.Error() != "portal activation is invalid" {
					t.Fatal("malformed capability initialized production or disclosed its input")
				}
			}
			t.Setenv(capability.setting, off)
			p, err := NewProduction(context.Background())
			if err != nil {
				t.Fatal("an explicitly disabled capability was refused")
			}
			p.(*provider).Shutdown()
		})
	}
}

// Production settings cannot escape the bounded identity-only staging lease.
func TestCapabilitySettingsDoNotEnableStagingOrWorkApproval(t *testing.T) {
	clearCapabilitySettings(t)
	for _, capability := range capabilityCases {
		t.Setenv(capability.setting, on)
	}
	t.Setenv("PORTAL_IDENTITY_PROFILE", "")
	client, _ := activeClient(context.Background(), t, time.Now().Add(time.Minute))
	for _, capability := range capabilityCases {
		if value, err := client.BooleanValue(context.Background(), capability.key, false, openfeature.NewEvaluationContext("", nil)); value || err == nil {
			t.Fatal("production capability settings broadened staging authority")
		}
	}
	client, _ = productionClient(context.Background(), t)
	t.Setenv("PORTAL_WORK_APPROVALS_FLAG", on)
	for _, key := range []string{"portal-work-approvals", "portal-worker-execution", "portal-unknown", "PORTAL_REQUESTS_FLAG"} {
		if value, err := client.BooleanValue(context.Background(), key, true, openfeature.NewEvaluationContext("", nil)); value || err != nil {
			t.Fatal("production configuration enabled work approval, execution or an unrecognized capability")
		}
	}
}
