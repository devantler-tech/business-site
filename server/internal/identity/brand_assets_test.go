package identity

import (
	"bytes"
	"os"
	"testing"
)

// TestBrandAssetsMatchTheSite keeps the portal's tab icon and wordmarks byte for
// byte equal to the public site's, so a logo change cannot leave the portal
// behind. The site's files are the source: public/favicon.png and the vector
// wordmarks in src/assets/brand.
func TestBrandAssetsMatchTheSite(t *testing.T) {
	for asset, source := range map[string]string{
		"icon.png":       "../../../public/favicon.png",
		"logo-dark.svg":  "../../../src/assets/brand/devantler-tech-logo-dark.svg",
		"logo-light.svg": "../../../src/assets/brand/devantler-tech-logo-light.svg",
	} {
		got, err := browserFiles.ReadFile("web/" + asset)
		if err != nil || len(got) == 0 {
			t.Fatalf("the portal has no %s: %v", asset, err)
		}
		want, err := os.ReadFile(source)
		if err != nil || len(want) == 0 {
			t.Fatalf("the site has no %s: %v", source, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("web/%s differs from %s; copy the site's file again", asset, source)
		}
	}
}
