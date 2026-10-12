package identity

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestRequestTemplateSelection exercises the actual response renderer. The
// raw-template lint exception must never leak fields into rendered attributes
// or turn false boolean attributes into selected options.
func TestRequestTemplateSelection(t *testing.T) {
	options := regexp.MustCompile(`<option value="([^"]+)"([^>]*)>`)
	for _, lang := range []string{"en", "da"} {
		for _, product := range []string{"website", "app", "service"} {
			t.Run(lang+"/"+product, func(t *testing.T) {
				response := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/portal/requests/new?lang="+lang, nil)
				new(browser).renderRequests(response, request, 400, "new", projectRequest{Product: product}, nil, "invalid")
				body := response.Body.String()
				if response.Code != 400 || !strings.Contains(body, `<html lang="`+lang+`">`) || strings.Contains(body, ".Request.Product") || strings.Contains(body, "{{") {
					t.Fatal("actual request renderer leaked template fields or lost presentation state")
				}
				start := strings.Index(body, `<select id="request-product"`)
				if start < 0 {
					t.Fatal("rendered offering selector missing")
				}
				end := strings.Index(body[start:], "</select>")
				if end < 0 {
					t.Fatal("rendered offering selector is incomplete")
				}
				matches := options.FindAllStringSubmatch(body[start:start+end], -1)
				if len(matches) != 3 {
					t.Fatal("rendered offering options changed")
				}
				selected := 0
				for _, option := range matches {
					attributes := strings.TrimSpace(option[2])
					if attributes == "selected" {
						selected++
						if option[1] != product {
							t.Fatal("renderer selected a different offering")
						}
					} else if attributes != "" {
						t.Fatal("renderer emitted an unexpected offering attribute")
					}
				}
				if selected != 1 {
					t.Fatal("renderer did not select exactly one offering")
				}
			})
		}
	}
}
