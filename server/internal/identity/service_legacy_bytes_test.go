package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

// These fixtures pin persisted wire contracts before any service fields are
// added. Expanding a domain struct must not invalidate an unchanged agreement.
func legacyServiceProposal() projectProposal {
	return projectProposal{
		Version: 7, Expected: 6, Key: "synthetic-draft",
		Scope:        "Scope <fixed> & \"quoted\"\nline\u2028end",
		Deliverables: "API \\ paths and æøå", Currency: "DKK",
		OneTimeTerms: "One-time <terms>", MonthlyTerms: "Monthly & ongoing",
		OneTimeMinor: 600000, MonthlyMinor: 30000,
		Prepared: time.Date(2026, 11, 16, 12, 34, 56, 123456789, time.FixedZone("synthetic", 19800)),
	}
}

func TestLegacyQuoteV1Bytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		proposal projectProposal
		wantJSON string
		wantHash string
	}{
		{name: "full", proposal: legacyServiceProposal(), wantJSON: `{"Scope":"Scope \u003c` +
			`fixed\u003e \u0026 \"quoted\"\nline\u2028end","Deliverables":"API \\ paths and æøå","Currency":"DKK","OneTimeTerms":"One-time \u003c` +
			`terms\u003e","MonthlyTerms":"Monthly \u0026 ongoing","OneTimeMinor":600000,"MonthlyMinor":30000}`, wantHash: "a1b134fe8788fe54c81adbff3b83bfbf23be5564a7182a2ffec86c416f53abab"},
		{name: "zero", wantJSON: `{"Scope":"","Deliverables":"","Currency":"","OneTimeTerms":"","MonthlyTerms":"","OneTimeMinor":0,"MonthlyMinor":0}`, wantHash: "f2c8bb388210b252dcf2a2a74686a62366c40ee83806db43385f4b1ff700ae59"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := proposalContentHash(tc.proposal)
			if err != nil {
				t.Fatal(err)
			}
			// The literal bytes and digest were captured from protected 414cf0b
			// before domain expansion, independently of the current encoder.
			sum := sha256.Sum256([]byte(tc.wantJSON))
			if hash != tc.wantHash || hex.EncodeToString(sum[:]) != tc.wantHash {
				t.Errorf("legacy quote digest = %s, want %s", hash, tc.wantHash)
			}
		})
	}
}

func legacyServiceEvidence() workEvidence {
	p := legacyServiceProposal()
	p.Prepared = p.Prepared.UTC()
	return workEvidence{
		Format: 2, Request: "synthetic-request", Client: "synthetic-client",
		Acceptance: "synthetic-acceptance", Event: 3,
		Proposal: p,
		Order: workOrder{
			ID: "synthetic-order", Collection: "invoice", OneTimeMinor: 600000, MonthlyMinor: 30000,
			MonthlyFrom: time.Date(2026, 12, 1, 0, 0, 0, 0, time.FixedZone("synthetic", 3600)).UTC(),
		},
		Funding: workFunding{
			Period: "acceptance", Reference: "synthetic-reference", GrossMinor: 600000, NetMinor: 480000, VATMinor: 120000,
			Settled: time.Date(2026, 11, 16, 18, 45, 0, 987654321, time.FixedZone("synthetic", -18000)).UTC(),
		},
		Acceptor: workMember{Issuer: "https://issuer.example", Subject: "client<&>", Client: "synthetic-client", Role: "client", Incarnation: "11111111-1111-4111-8111-111111111111"},
		Actor:    workMember{Issuer: "https://issuer.example", Subject: "operator", Client: "operator-workspace", Role: "operator", Incarnation: "22222222-2222-4222-8222-222222222222"},
	}
}

func TestLegacyWorkEvidenceV2Bytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence workEvidence
		wantJSON string
		wantHash string
	}{
		{name: "full", evidence: legacyServiceEvidence(), wantJSON: `{"Format":2,"Request":"synthetic-request","Client":"synthetic-client","Acceptance":"synthetic-acceptance","Event":3,"Hash":"a1b134fe8788fe54c81adbff3b83bfbf23be5564a7182a2ffec86c416f53abab","Proposal":{"Version":7,"Expected":6,"Key":"synthetic-draft","Scope":"Scope \u003c` +
			`fixed\u003e \u0026 \"quoted\"\nline\u2028end","Deliverables":"API \\ paths and æøå","Currency":"DKK","OneTimeTerms":"One-time \u003c` +
			`terms\u003e","MonthlyTerms":"Monthly \u0026 ongoing","OneTimeMinor":600000,"MonthlyMinor":30000,"Prepared":"2026-11-16T07:04:56.123456789Z"},"Order":{"ID":"synthetic-order","Collection":"invoice","OneTimeMinor":600000,"MonthlyMinor":30000,"MonthlyFrom":"2026-11-30T23:00:00Z"},"Funding":{"Period":"acceptance","Reference":"synthetic-reference","GrossMinor":600000,"NetMinor":480000,"VATMinor":120000,"Settled":"2026-11-16T23:45:00.987654321Z"},"Acceptor":{"Issuer":"https://issuer.example","Subject":"client\u003c\u0026\u003e","Client":"synthetic-client","Role":"client","Incarnation":"11111111-1111-4111-8111-111111111111"},"Actor":{"Issuer":"https://issuer.example","Subject":"operator","Client":"operator-workspace","Role":"operator","Incarnation":"22222222-2222-4222-8222-222222222222"}}`, wantHash: "aa4b5f90e749ccada9fd5241cc80a632cfef7df687fa42dc078a7bd70766efd3"},
		{name: "zero", evidence: workEvidence{Format: 2}, wantJSON: `{"Format":2,"Request":"","Client":"","Acceptance":"","Event":0,"Hash":"f2c8bb388210b252dcf2a2a74686a62366c40ee83806db43385f4b1ff700ae59","Proposal":{"Version":0,"Expected":0,"Key":"","Scope":"","Deliverables":"","Currency":"","OneTimeTerms":"","MonthlyTerms":"","OneTimeMinor":0,"MonthlyMinor":0,"Prepared":"0001-01-01T00:00:00Z"},"Order":{"ID":"","Collection":"","OneTimeMinor":0,"MonthlyMinor":0,"MonthlyFrom":"0001-01-01T00:00:00Z"},"Funding":{"Period":"","Reference":"","GrossMinor":0,"NetMinor":0,"VATMinor":0,"Settled":"0001-01-01T00:00:00Z"},"Acceptor":{"Issuer":"","Subject":"","Client":"","Role":"","Incarnation":""},"Actor":{"Issuer":"","Subject":"","Client":"","Role":"","Incarnation":""}}`, wantHash: "532abee3ce4b464ce951333e15629678534be86bdb88d17ebe3792b1cabcf765"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			tc.evidence.Hash, err = proposalContentHash(tc.evidence.Proposal)
			if err != nil {
				t.Fatal(err)
			}
			// This is the unchanged protected collectWorkEvidence encoder.
			data, err := json.Marshal(tc.evidence)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			if string(data) != tc.wantJSON || hash != tc.wantHash {
				t.Errorf("old evidence baseline: bytes=%s\nsha256=%s", data, hash)
			}
		})
	}
}
