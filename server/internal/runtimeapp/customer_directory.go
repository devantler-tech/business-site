package runtimeapp

import (
	"os"
	"path/filepath"

	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/devantler-tech/business-site/server/internal/zitadel"
)

type customerDirectoryConfig struct{ url, org, tokenFile string }

func readCustomerDirectoryConfig(getenv func(string) string) (customerDirectoryConfig, error) {
	c := customerDirectoryConfig{getenv("PORTAL_ZITADEL_URL"), getenv("PORTAL_ZITADEL_ORG_ID"), getenv("PORTAL_ZITADEL_TOKEN_PROJECTION_FILE")}
	if c == (customerDirectoryConfig{}) {
		return c, nil
	}
	if !filepath.IsAbs(c.tokenFile) {
		return customerDirectoryConfig{}, ErrConfig
	}
	if _, err := zitadel.New(c.url, c.org, func() (string, error) { return "", ErrConfig }, nil); err != nil {
		return customerDirectoryConfig{}, ErrConfig
	}
	return c, nil
}

// linkCustomerDirectory does not read a credential at startup. Every operation
// takes a fresh bounded snapshot of the confined projection, admitting rotation.
func linkCustomerDirectory(f *identity.Foundation, c customerDirectoryConfig) error {
	if c == (customerDirectoryConfig{}) {
		return nil
	}
	if os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return ErrConfig
	}
	client, err := zitadel.New(c.url, c.org, func() (string, error) {
		data, err := projectionBytes(c.tokenFile)
		if err != nil {
			return "", ErrConfig
		}
		return trimInputLineEnding(string(data)), nil
	}, nil)
	if err != nil || f.LinkCustomerDirectory(client) != nil {
		return ErrConfig
	}
	return nil
}
