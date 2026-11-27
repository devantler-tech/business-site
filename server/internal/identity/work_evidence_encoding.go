package identity

import (
	"encoding/json"
	"time"
)

// These closed wire types preserve existing stored format-2 evidence. Domain
// metadata can expand without retroactively changing a customer's agreement.
type proposalEvidenceV1 struct {
	Version, Expected                                              int
	Key, Scope, Deliverables, Currency, OneTimeTerms, MonthlyTerms string
	OneTimeMinor, MonthlyMinor                                     int64
	Prepared                                                       time.Time
}

type orderEvidenceV2 struct {
	ID, Collection             string
	OneTimeMinor, MonthlyMinor int64
	MonthlyFrom                time.Time
}

type fundingEvidenceV2 struct {
	Period, Reference              string
	GrossMinor, NetMinor, VATMinor int64
	Settled                        time.Time
}

type memberEvidenceV2 struct {
	Issuer, Subject, Client, Role, Incarnation string
}

type workEvidenceV2 struct {
	Format          int
	Request, Client string
	Acceptance      string
	Event           int
	Hash            string
	Proposal        proposalEvidenceV1
	Order           orderEvidenceV2
	Funding         fundingEvidenceV2
	Acceptor, Actor memberEvidenceV2
}

type workEvidenceV3 struct {
	workEvidenceV2
	ServiceKind proposalServiceKind
}

func proposalEvidenceLegacy(d projectProposal) proposalEvidenceV1 {
	return proposalEvidenceV1{
		Version: d.Version, Expected: d.Expected, Key: d.Key,
		Scope: d.Scope, Deliverables: d.Deliverables, Currency: d.Currency,
		OneTimeTerms: d.OneTimeTerms, MonthlyTerms: d.MonthlyTerms,
		OneTimeMinor: d.OneTimeMinor, MonthlyMinor: d.MonthlyMinor, Prepared: d.Prepared,
	}
}

// MarshalJSON uses format 2 for the original unclassified proposals. Format 3
// adds the explicit software kind and binds the hash of the typed quote.
func (e workEvidence) MarshalJSON() ([]byte, error) {
	wire := workEvidenceV2{
		Format: e.Format, Request: e.Request, Client: e.Client,
		Acceptance: e.Acceptance, Event: e.Event, Hash: e.Hash,
		Proposal: proposalEvidenceLegacy(e.Proposal),
		Order:    orderEvidenceV2{e.Order.ID, e.Order.Collection, e.Order.OneTimeMinor, e.Order.MonthlyMinor, e.Order.MonthlyFrom},
		Funding:  fundingEvidenceV2{e.Funding.Period, e.Funding.Reference, e.Funding.GrossMinor, e.Funding.NetMinor, e.Funding.VATMinor, e.Funding.Settled},
		Acceptor: memberEvidenceV2(e.Acceptor), Actor: memberEvidenceV2(e.Actor),
	}
	switch {
	case e.Format == 2 && e.Proposal.ContentFormat >= 0 && e.Proposal.ContentFormat <= 1 && e.Proposal.Service == (proposalServiceTerms{}):
		return json.Marshal(wire)
	case e.Format == 3 && e.Proposal.ContentFormat == 2 && e.Proposal.ContentSHA256 == e.Hash && e.Proposal.Service.Kind == proposalServiceSoftware && e.Proposal.Service.valid():
		return json.Marshal(workEvidenceV3{workEvidenceV2: wire, ServiceKind: e.Proposal.Service.Kind})
	default:
		return nil, errProposalConflict
	}
}
