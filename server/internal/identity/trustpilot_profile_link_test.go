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

var errTrustpilotProfileBotDenied = errors.New("automated access denied by Trustpilot for the Devantler Tech profile")

func checkTrustpilotProfile(profile *http.Response) error {
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
	return errTrustpilotProfileBotDenied
}

func auditTrustpilotProfileLink(output string, get func(string) (*http.Response, error)) (string, error) {
	href := ` href="` + trustpilotProfileURL + `"`
	if !strings.Contains(output, href) {
		return output, nil
	}
	profile, err := get(trustpilotProfileURL)
	if err != nil {
		return "", fmt.Errorf("open Trustpilot profile: %w", err)
	}
	defer func() { _ = profile.Body.Close() }()
	profileErr := checkTrustpilotProfile(profile)
	if profileErr == nil {
		// Lychee checks the ordinary link when the profile serves bots.
		return output, nil
	}
	if !errors.Is(profileErr, errTrustpilotProfileBotDenied) {
		return "", profileErr
	}

	// The exact public profile was opened in a regular browser by its owner.
	// Here, confirm separately that the provider still serves this business.
	// The review form can never turn a blocked profile probe into a pass.
	review, err := get(trustpilotReviewURL)
	if err != nil {
		return "", fmt.Errorf("open Trustpilot review page: %w", err)
	}
	defer func() { _ = review.Body.Close() }()
	if review.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected Trustpilot review page HTTP %d", review.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(review.Body, 1<<20))
	if err != nil || !strings.Contains(string(page), "<title>Rate Devantler Tech</title>") {
		return "", errors.New("could not confirm Devantler Tech on the Trustpilot review page")
	}
	return strings.ReplaceAll(output, href, ` data-trustpilot-profile-status="automation-blocked"`), nil
}

func TestPortalLinkTrustpilotProfileCoverage(t *testing.T) {
	const botDenial = `{"error":true,"message":"Your request has been blocked as it has been identified as coming from a bot."}`
	const title = "<title>Rate Devantler Tech</title>"
	for _, test := range []struct {
		name                   string
		profile, review, calls int
		denial, page           string
		ok, automationBlocked  bool
	}{
		{"public profile retains href for Lychee", 200, 0, 1, "", "", true, false},
		{"recognized denial and same business", 403, 200, 2, botDenial, title, true, true},
		{"unknown denial", 403, 200, 1, `{"error":true,"message":"Forbidden"}`, title, false, false},
		{"missing profile", 404, 200, 1, "", title, false, false},
		{"missing review page", 403, 404, 2, botDenial, title, false, false},
		{"different business", 403, 200, 2, botDenial, "<title>Rate Another Company</title>", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			const markup = `<a href="` + trustpilotProfileURL + `">Reviews</a><a href="https://example.test/">Other</a>`
			output, err := auditTrustpilotProfileLink(markup, func(target string) (*http.Response, error) {
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
			if !test.ok {
				return
			}
			want := markup
			if test.automationBlocked {
				want = `<a data-trustpilot-profile-status="automation-blocked">Reviews</a><a href="https://example.test/">Other</a>`
			}
			if output != want {
				t.Fatalf("rendered link evidence mismatch: %s", output)
			}
		})
	}
	t.Run("bot denial is never an automated profile pass", func(t *testing.T) {
		profile := &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(botDenial)),
		}
		if err := checkTrustpilotProfile(profile); !errors.Is(err, errTrustpilotProfileBotDenied) {
			t.Fatalf("expected automated profile denial, got %v", err)
		}
	})
}
