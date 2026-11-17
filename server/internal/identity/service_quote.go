package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

type proposalServiceKind string

const (
	proposalServiceSoftware    proposalServiceKind = "software"
	proposalServiceAppointment proposalServiceKind = "appointment"
)

// proposalServiceTerms is immutable accepted quote content. Neither these terms
// nor their digest is an execution or reservation grant.
type proposalServiceTerms struct {
	Kind                   proposalServiceKind
	DurationMinutes        int
	AppointmentTermsEN     string
	AppointmentTermsDA     string
	AppointmentTermsSHA256 string
}

func appointmentTermsHash(english, danish string) (string, error) {
	data, err := json.Marshal(struct{ English, Danish string }{english, danish})
	if err != nil {
		return "", errTriageStorage
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (s proposalServiceTerms) valid() bool {
	switch s.Kind {
	case proposalServiceSoftware:
		return s.DurationMinutes == 0 && s.AppointmentTermsEN == "" && s.AppointmentTermsDA == "" && s.AppointmentTermsSHA256 == ""
	case proposalServiceAppointment:
		// Bound before any future conversion to time.Duration. This is the
		// booking engine's representation ceiling, not availability or a sale.
		if s.DurationMinutes <= 0 || s.DurationMinutes > 1440 || !validRequestText(s.AppointmentTermsEN, 2000, true) || !validRequestText(s.AppointmentTermsDA, 2000, true) {
			return false
		}
		hash, err := appointmentTermsHash(s.AppointmentTermsEN, s.AppointmentTermsDA)
		return err == nil && hash == s.AppointmentTermsSHA256
	default:
		return false
	}
}

func parseProposalService(form url.Values) (int, proposalServiceTerms, bool) {
	if !form.Has("service_kind") {
		for _, name := range []string{"appointment_minutes", "appointment_terms_en", "appointment_terms_da"} {
			if form.Has(name) {
				return 0, proposalServiceTerms{}, false
			}
		}
		return 1, proposalServiceTerms{}, true
	}
	s := proposalServiceTerms{Kind: proposalServiceKind(form.Get("service_kind"))}
	if s.Kind == proposalServiceSoftware {
		return 2, s, form.Get("appointment_minutes") == "" && form.Get("appointment_terms_en") == "" && form.Get("appointment_terms_da") == ""
	}
	if s.Kind != proposalServiceAppointment {
		return 0, proposalServiceTerms{}, false
	}
	minutes, ok := proposalNumber(form.Get("appointment_minutes"), 1440)
	if !ok || minutes == 0 {
		return 0, proposalServiceTerms{}, false
	}
	s.DurationMinutes = minutes
	for _, field := range []struct {
		name   string
		target *string
	}{{"appointment_terms_en", &s.AppointmentTermsEN}, {"appointment_terms_da", &s.AppointmentTermsDA}} {
		text := strings.ReplaceAll(strings.ReplaceAll(form.Get(field.name), "\r\n", "\n"), "\r", "\n")
		if !validRequestText(text, 2000, true) {
			return 0, proposalServiceTerms{}, false
		}
		*field.target = strings.TrimSpace(text)
	}
	hash, err := appointmentTermsHash(s.AppointmentTermsEN, s.AppointmentTermsDA)
	if err != nil {
		return 0, proposalServiceTerms{}, false
	}
	s.AppointmentTermsSHA256 = hash
	return 2, s, s.valid()
}

func serviceProposalContentHash(d projectProposal) (string, error) {
	if d.ContentFormat != 2 || !d.Service.valid() {
		return "", errProposalConflict
	}
	// Draft metadata and recorded provenance are bound separately from these
	// explicitly versioned commercial fields.
	content := struct {
		ContentFormat                                             int
		Scope, Deliverables, Currency, OneTimeTerms, MonthlyTerms string
		OneTimeMinor, MonthlyMinor                                int64
		Service                                                   proposalServiceTerms
	}{2, d.Scope, d.Deliverables, d.Currency, d.OneTimeTerms, d.MonthlyTerms, d.OneTimeMinor, d.MonthlyMinor, d.Service}
	data, err := json.Marshal(content)
	if err != nil {
		return "", errTriageStorage
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
