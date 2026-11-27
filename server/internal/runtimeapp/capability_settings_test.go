package runtimeapp

import (
	"context"
	"os"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
)

// A missing projection key must break the actual projection-to-provider path.
func TestProjectedProductionCapabilitySettings(t *testing.T) {
	unsetPortalEnvironment(t)
	settings := fictionalDependencies()
	settings["PORTAL_IDENTITY_PROFILE"] = "production"
	settings["PORTAL_IDENTITY_FLAG"] = "{\"defaultVariant\":\"on\",\"variants\":[{\"name\":\"on\",\"value\":true}]}"
	productionSettings := []string{
		"PORTAL_CONTACT_VERIFICATION_FLAG", "PORTAL_REQUESTS_FLAG", "PORTAL_TRIAGE_FLAG", "PORTAL_SUPPORT_FLAG",
		"PORTAL_PROPOSALS_FLAG", "PORTAL_PROPOSAL_SHARING_FLAG", "PORTAL_PROPOSAL_ACCEPTANCE_FLAG",
		"PORTAL_INVOICE_BILLING_FLAG", "PORTAL_CARD_BILLING_FLAG",
	}
	input := settingsText(settings)
	for _, setting := range productionSettings {
		input += setting + "=" + settings["PORTAL_IDENTITY_FLAG"] + "\n"
	}
	if err := ApplyPortalSettingsProjection(projectedIdentityFixture(t, []byte(input), 0440)); err != nil {
		t.Fatal("named production capability settings were refused by the real projection reader")
	}
	cfg, err := ReadConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := IdentityProvider(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.(interface{ Shutdown() }).Shutdown)
	for _, capability := range []string{
		"portal-contact-verification", "portal-requests", "portal-triage", "portal-support", "portal-proposals",
		"portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoice-billing", "portal-card-billing",
	} {
		result := provider.BooleanEvaluation(t.Context(), capability, false, openfeature.FlattenedContext{})
		if !result.Value || result.ResolutionDetail().ErrorCode != "" {
			t.Fatalf("%s did not reach the maintained provider from its projection", capability)
		}
	}
	result := provider.BooleanEvaluation(t.Context(), "portal-work-approvals", true, openfeature.FlattenedContext{})
	if result.Value || result.ResolutionDetail().ErrorCode != "" {
		t.Fatal("the capability projection enabled separate work-approval authority")
	}
}
