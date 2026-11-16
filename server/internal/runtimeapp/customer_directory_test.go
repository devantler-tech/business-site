package runtimeapp

import (
	"errors"
	"testing"
)

func TestCustomerDirectoryConfigurationIsOptionalAndComplete(t *testing.T) {
	keys := []string{"PORTAL_ZITADEL_URL", "PORTAL_ZITADEL_ORG_ID", "PORTAL_ZITADEL_TOKEN_PROJECTION_FILE"}
	settings := map[string]string{keys[0]: "https://accounts.example.invalid", keys[1]: "customers", keys[2]: "/private/customer-directory/token"}
	if _, err := ReadConfig(settingsEnv(withPortal(settings))); err != nil {
		t.Fatal("complete customer directory refused")
	}
	for _, missing := range keys {
		t.Run(missing, func(t *testing.T) {
			values := withPortal(settings)
			delete(values, missing)
			if _, err := ReadConfig(settingsEnv(values)); !errors.Is(err, ErrConfig) {
				t.Fatal("partial customer directory accepted")
			}
		})
	}
	for key, value := range map[string]string{keys[0]: "http://accounts.example.invalid", keys[1]: "foreign/org", keys[2]: "relative-token"} {
		values := withPortal(settings)
		values[key] = value
		if _, err := ReadConfig(settingsEnv(values)); !errors.Is(err, ErrConfig) {
			t.Error("unsafe customer directory accepted")
		}
	}
	if _, err := ReadConfig(settingsEnv(settings)); !errors.Is(err, ErrConfig) {
		t.Error("customer directory without portal accepted")
	}
	if _, err := ReadConfig(settingsEnv(withPortal(nil))); err != nil {
		t.Fatal("optional directory became mandatory")
	}
	requireProjected(t, keys...)
}
