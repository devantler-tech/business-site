//go:build rendererintegration

package contactrenderer

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// TestCompiledComponent is explicitly selected by the Node-24 build test. A
// missing artifact fails, never silently skips. Ordinary Go/DB tests need no Node.
func TestCompiledComponent(t *testing.T) {
	s, err := New(os.Getenv("RENDERER_TEST_NODE"), os.Getenv("RENDERER_TEST_ARTIFACT"), flags(t, true).NewClient())
	if err != nil {
		t.Fatal("actual compiled artifact is required")
	}
	t.Cleanup(func() { s.Close() })
	// Cold start belongs to the actual 250ms render, not a hidden warm-up.
	c := projection()
	c.Verification = "confirmable"
	c.Text.VerifyLabel = "Confirm"
	c.Text.VerificationHelp = "Confirm your saved address"
	first, err := s.Render(t.Context(), c)
	if err != nil || !strings.Contains(first, "<main id=\"main\">") || !strings.Contains(first, "9223372036854775807") {
		t.Fatal("actual cold compiled form did not render", err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			c := projection()
			c.Locale = "da"
			c.Name = "Fictional <script>private</script>"
			c.CSRF = "nonce\" autofocus=\"true"
			c.Text.Title = "Dine kontaktoplysninger"
			c.Text.SaveLabel = "Gem kontaktoplysninger"
			c.Text.ReloadLabel = "Genindlæs kontaktoplysninger"
			body, err := s.Render(t.Context(), c)
			if err != nil || !strings.Contains(body, "name=\"lang\" value=\"da\"") ||
				!strings.Contains(body, "<h1>Dine kontaktoplysninger</h1>") ||
				!strings.Contains(body, "nonce&quot; autofocus=&quot;true") ||
				strings.Contains(body, "<script") || strings.Contains(body, " autofocus=\"true\"") {
				t.Errorf("actual concurrent localized form failed %d: %v", i, err)
			}
		})
	}
	wg.Wait()
}
