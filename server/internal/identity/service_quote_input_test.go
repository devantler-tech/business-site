package identity

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func serviceQuoteForm(kind string) url.Values {
	p := legacyServiceProposal()
	f := url.Values{
		"submission_key": {"AAAAAAAAAAAAAAAAAAAAAAAAAA"}, "expected_version": {"0"},
		"scope": {p.Scope}, "deliverables": {p.Deliverables}, "currency": {"DKK"},
		"one_time_dkk": {"6000"}, "monthly_dkk": {"300"},
		"one_time_terms": {p.OneTimeTerms}, "monthly_terms": {p.MonthlyTerms},
		"service_kind": {kind},
	}
	if kind == "appointment" {
		f.Set("appointment_minutes", "30")
		f.Set("appointment_terms_en", "A synthetic agreed appointment. Changes require a new agreement.")
		f.Set("appointment_terms_da", "En syntetisk aftalt samtale. Ændringer kræver en ny aftale.")
	}
	return f
}

func parseServiceQuoteForm(form url.Values) (projectProposal, bool) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return parseProposal(r)
}

// A valid typed human quote must retain its exact ordinary agreement fields;
// accepting only legacy input or silently dropping the service contract fails.
func TestServiceQuoteV2Input(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   string
		change func(url.Values)
		want   bool
	}{
		{name: "legacy", kind: "software", change: func(f url.Values) { f.Del("service_kind") }, want: true},
		{name: "software", kind: "software", want: true},
		{name: "appointment", kind: "appointment", want: true},
		{name: "representation ceiling", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "1440") }, want: true},
		{name: "unknown kind", kind: "other"},
		{name: "wrong case", kind: "Software"},
		{name: "missing kind", kind: "appointment", change: func(f url.Values) { f.Del("service_kind") }},
		{name: "software duration", kind: "software", change: func(f url.Values) { f.Set("appointment_minutes", "30") }},
		{name: "software terms", kind: "software", change: func(f url.Values) { f.Set("appointment_terms_en", "Unaccepted appointment terms") }},
		{name: "missing duration", kind: "appointment", change: func(f url.Values) { f.Del("appointment_minutes") }},
		{name: "zero duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "0") }},
		{name: "negative duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "-1") }},
		{name: "fractional duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "1.5") }},
		{name: "ambiguous duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "030") }},
		{name: "oversized duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "1441") }},
		{name: "overflow duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", strings.Repeat("9", 100)) }},
		{name: "missing English terms", kind: "appointment", change: func(f url.Values) { f.Del("appointment_terms_en") }},
		{name: "missing Danish terms", kind: "appointment", change: func(f url.Values) { f.Del("appointment_terms_da") }},
		{name: "bounded terms", kind: "appointment", change: func(f url.Values) { f.Set("appointment_terms_en", strings.Repeat("x", 2001)) }},
		{name: "control in terms", kind: "appointment", change: func(f url.Values) { f.Set("appointment_terms_da", "unsafe\x00terms") }},
		{name: "submitted hash", kind: "appointment", change: func(f url.Values) { f.Set("appointment_terms_sha256", strings.Repeat("a", 64)) }},
		{name: "submitted actor", kind: "appointment", change: func(f url.Values) { f.Set("operator_incarnation", "11111111-1111-4111-8111-111111111111") }},
		{name: "duplicate kind", kind: "software", change: func(f url.Values) { f.Add("service_kind", "appointment") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := serviceQuoteForm(tc.kind)
			if tc.change != nil {
				tc.change(form)
			}
			d, ok := parseServiceQuoteForm(form)
			if ok != tc.want {
				t.Fatalf("valid typed quote = %v, want %v", ok, tc.want)
			}
			if ok {
				want := legacyServiceProposal()
				if d.Scope != want.Scope || d.Deliverables != want.Deliverables || d.OneTimeTerms != want.OneTimeTerms || d.MonthlyTerms != want.MonthlyTerms || d.Currency != want.Currency || d.OneTimeMinor != want.OneTimeMinor || d.MonthlyMinor != want.MonthlyMinor {
					t.Fatal("typed quote changed the ordinary agreement")
				}
			}
		})
	}
}

func TestServiceQuoteV2HashBindsContract(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range []struct {
		name   string
		kind   string
		change func(url.Values)
	}{
		{name: "software", kind: "software"},
		{name: "appointment", kind: "appointment"},
		{name: "changed duration", kind: "appointment", change: func(f url.Values) { f.Set("appointment_minutes", "45") }},
		{name: "changed English terms", kind: "appointment", change: func(f url.Values) { f.Set("appointment_terms_en", "Separately agreed synthetic English terms.") }},
		{name: "changed Danish terms", kind: "appointment", change: func(f url.Values) { f.Set("appointment_terms_da", "Særskilt aftalte syntetiske danske vilkår.") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := serviceQuoteForm(tc.kind)
			if tc.change != nil {
				tc.change(form)
			}
			d, ok := parseServiceQuoteForm(form)
			if !ok {
				t.Fatal("valid service contract was refused")
			}
			hash, err := proposalContentHash(d)
			if err != nil {
				t.Fatal(err)
			}
			if hash == "a1b134fe8788fe54c81adbff3b83bfbf23be5564a7182a2ffec86c416f53abab" || seen[hash] {
				t.Fatal("quote hash omitted a client-visible service term")
			}
			seen[hash] = true
		})
	}
}
