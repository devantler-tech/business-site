package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const trustpilotProfileURL = "https://www.trustpilot.com/review/devantler.tech"
const trustpilotReviewURL = "https://www.trustpilot.com/evaluate/devantler.tech"

// Trustpilot refuses link checker traffic to profiles, including confirmed public
// profiles. Check the fixed profile first and corroborate only that specific
// denial with a live review page identifying this business.
func checkTrustpilotProfile(get func(string) (*http.Response, error)) error {
	profile, err := get(trustpilotProfileURL)
	if err != nil {
		return fmt.Errorf("open Trustpilot profile: %w", err)
	}
	defer func() { _ = profile.Body.Close() }()
	if profile.StatusCode == http.StatusOK {
		return nil
	}
	var denial struct {
		Error   bool
		Message string
	}
	if profile.StatusCode != http.StatusForbidden ||
		!strings.HasPrefix(profile.Header.Get("Content-Type"), "application/json") ||
		json.NewDecoder(io.LimitReader(profile.Body, 4096)).Decode(&denial) != nil ||
		!denial.Error || !strings.Contains(denial.Message, "identified as coming from a bot") {
		return fmt.Errorf("unexpected Trustpilot profile HTTP %d", profile.StatusCode)
	}

	review, err := get(trustpilotReviewURL)
	if err != nil {
		return fmt.Errorf("open Trustpilot review page: %w", err)
	}
	defer func() { _ = review.Body.Close() }()
	if review.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected Trustpilot review page HTTP %d", review.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(review.Body, 1<<20))
	if err != nil || !strings.Contains(string(page), "<title>Rate Devantler Tech</title>") {
		return errors.New("could not confirm Devantler Tech on the Trustpilot review page")
	}
	return nil
}

func TestPortalLinkTrustpilotProfileCoverage(t *testing.T) {
	const botDenial = `{"error":true,"message":"Your request has been blocked as it has been identified as coming from a bot."}`
	const title = "<title>Rate Devantler Tech</title>"
	for _, test := range []struct {
		name                   string
		profile, review, calls int
		denial, page           string
		ok                     bool
	}{
		{"public profile", 200, 0, 1, "", "", true},
		{"recognized denial and same business", 403, 200, 2, botDenial, title, true},
		{"unknown denial", 403, 200, 1, `{"error":true,"message":"Forbidden"}`, title, false},
		{"missing profile", 404, 200, 1, "", title, false},
		{"missing review page", 403, 404, 2, botDenial, title, false},
		{"different business", 403, 200, 2, botDenial, "<title>Rate Another Company</title>", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			err := checkTrustpilotProfile(func(target string) (*http.Response, error) {
				calls++
				switch target {
				case trustpilotProfileURL:
					return &http.Response{
						StatusCode: test.profile,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(test.denial)),
					}, nil
				case trustpilotReviewURL:
					return &http.Response{
						StatusCode: test.review,
						Body:       io.NopCloser(strings.NewReader(test.page)),
					}, nil
				default:
					return nil, fmt.Errorf("unexpected URL %q", target)
				}
			})
			if (err == nil) != test.ok || calls != test.calls {
				t.Fatalf("profile check result: requests=%d, error=%v", calls, err)
			}
		})
	}
}
