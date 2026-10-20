//go:build rendererintegration

package identity

import (
	"os"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/contactrenderer"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// TestCompiledContactMaintenance checks actual compiled body output, not an
// authenticated response. The Go authority/browser journeys remain separate.
func TestCompiledContactMaintenance(t *testing.T) {
	api := flagAPI(t, true)
	if err := api.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(contactRendererFlags(true))); err != nil {
		t.Fatal(err)
	}
	renderer, err := contactrenderer.New(os.Getenv("RENDERER_TEST_NODE"), os.Getenv("RENDERER_TEST_ARTIFACT"), api.NewClient())
	if err != nil {
		t.Fatal("actual Node executable and compiled artifact required")
	}
	t.Cleanup(renderer.Close)
	count := 0
	for _, lang := range []string{"en", "da"} {
		for _, view := range maintenanceContactViews {
			page := maintenancePage(lang, view)
			body, err := renderer.Render(t.Context(), contactProjection(page, maintenanceProblem(view)))
			if err != nil {
				t.Fatalf("compiled view=%s language=%s failed: %v", view, lang, err)
			}
			if err := checkMaintenanceContact(body, lang, *maintenanceTask); err != nil {
				t.Fatalf("compiled view=%s language=%s: %v", view, lang, err)
			}
			if err := checkMaintenanceCompiledState(body, page, *maintenanceTask); err != nil {
				t.Fatalf("compiled view=%s language=%s: %v", view, lang, err)
			}
			for _, target := range []string{"main", "intro", "heading", "section", "reload", "workspace"} {
				t.Run(lang+"/"+view+"/"+target, func(t *testing.T) {
					bad := maintenanceAlterBody(t, body, lang, target)
					if checkMaintenanceCompiledState(bad, page, *maintenanceTask) == nil {
						t.Fatal("incomplete actual compiled contact body was accepted")
					}
				})
			}
			for name, extra := range map[string]string{
				"outside section":    `<aside>Unexpected content</aside>`,
				"outside script":     `<script src="/unexpected.js"></script>`,
				"outside style":      `<style>main{display:none}</style>`,
				"outside stylesheet": `<link rel="stylesheet" href="/unexpected.css">`,
				"outside text":       `Unexpected trailing content`,
			} {
				t.Run(lang+"/"+view+"/"+name, func(t *testing.T) {
					if checkMaintenanceCompiledState(body+extra, page, *maintenanceTask) == nil {
						t.Fatal("active content outside the compiled main was accepted")
					}
				})
			}
			count++
		}
	}
	if count != 12 {
		t.Fatal("incomplete compiled contact inventory")
	}
	t.Logf("actual compiled Svelte bodies=%d", count)
}
