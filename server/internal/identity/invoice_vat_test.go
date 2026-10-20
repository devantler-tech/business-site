package identity

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestInvoiceExplicitTaxComponents rejects missing, imprecise and inconsistent
// external facts instead of deriving tax from the total or proposal.
func TestInvoiceExplicitTaxComponents(t *testing.T) {
	for _, tc := range []struct {
		name, net, vat, total string
		want                  bool
	}{
		{"domestic", "3000.00", "750.00", "3750.00", true},
		{"decimal comma", "0,80", "0,20", "1", true},
		{"explicit zero VAT", "1234.56", "0", "1234.56", true},
		{"maximum", "10000000", "0.00", "10000000.00", true},
		// A legacy retry has no component keys. Only the stored-row writer can
		// distinguish that exact old receipt from an invalid new record.
		{"legacy retry shape", "", "", "3750", true},
		{"missing net", "", "750", "3750", false},
		{"missing VAT", "3000", "", "3750", false},
		{"inconsistent", "3000", "750.01", "3750", false},
		{"negative net", "-1", "2", "1", false},
		{"negative VAT", "2", "-1", "1", false},
		{"fractional minor unit", "0.801", "0.199", "1", false},
		{"grouped", "3,000.00", "750", "3750", false},
		{"exponent", "3e3", "750", "3750", false},
		{"overflow", "9223372036854775807", "1", "1", false},
		{"above bound", "10000000.01", "0", "10000000", false},
		{"sum above bound", "10000000", "10000000", "10000000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{
				"invoice_expected": {"0"}, "invoice_key": {"AAAAAAAAAAAAAAAAAAAAAAAAAA"},
				"invoice_acceptance": {"BBBBBBBBBBBBBBBBBBBBBBBBBB"},
				"reference":          {"FICTIONAL-01"}, "purpose": {"Fictional issued invoice"},
				"amount_dkk": {tc.total},
			}
			if tc.net != "" {
				form.Set("net_dkk", tc.net)
			}
			if tc.vat != "" {
				form.Set("vat_dkk", tc.vat)
			}
			_, valid := parseInvoice(httptestInvoiceForm(form))
			if valid != tc.want {
				t.Fatalf("explicit external components admitted=%t, want %t", valid, tc.want)
			}
		})
	}
}

// TestInvoiceTaxDurableBinding catches lost components, derived tax and retries
// which alter the split while retaining the same payable total.
func TestInvoiceTaxDurableBinding(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	f.Set("net_dkk", "3000,00")
	f.Set("vat_dkk", "750.00")
	f.Set("amount_dkk", "3750.00")
	path := "/portal/operator/requests/" + proposalRequestID
	for range 2 {
		if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 303 {
			t.Fatalf("explicit invoice or identical retry refused: %d", status)
		}
	}
	var net, vat, total int64
	if err := b.pool.QueryRow(t.Context(), `SELECT net_minor,vat_minor,amount_minor FROM portal_invoices`).Scan(&net, &vat, &total); err != nil || net != 300000 || vat != 75000 || total != 375000 {
		t.Fatal("explicit invoice components not stored exactly", err)
	}
	f.Set("net_dkk", "2999")
	f.Set("vat_dkk", "751")
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 409 {
		t.Fatal("retry reclassified the original invoice")
	}
	f.Del("net_dkk")
	f.Del("vat_dkk")
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 409 {
		t.Fatal("new explicit invoice accepted a legacy-shaped retry")
	}
	b.reconnect(t.Context())
	for _, tc := range []struct{ lang, net, vat, total string }{
		{"en", "3000.00 DKK", "750.00 DKK", "3750.00 DKK"},
		{"da", "3000,00 DKK", "750,00 DKK", "3750,00 DKK"},
	} {
		status, body, headers := b.request("GET", path+"?lang="+tc.lang, nil, "")
		if status != 200 || headers.Get("Cache-Control") != "no-store" || !strings.Contains(body, tc.net) || !strings.Contains(body, tc.vat) || !strings.Contains(body, tc.total) {
			t.Fatal("localized durable invoice breakdown missing")
		}
	}
}

// TestInvoiceTaxMissingNewAndMalformedFields keeps absent components exclusively
// for an existing legacy receipt, never a new invoice or a blank-field shortcut.
func TestInvoiceTaxMissingNewAndMalformedFields(t *testing.T) {
	b := invoiceFixture(t)
	for _, mode := range []string{"absent", "empty", "partial", "duplicate-net", "duplicate-vat", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := invoiceForm(t, b)
			f.Set("net_dkk", "1000")
			f.Set("vat_dkk", "234.56")
			switch mode {
			case "absent":
				f.Del("net_dkk")
				f.Del("vat_dkk")
			case "empty":
				f.Set("net_dkk", "")
				f.Set("vat_dkk", "")
			case "partial":
				f.Del("vat_dkk")
			case "duplicate-net":
				f.Add("net_dkk", "1000")
			case "duplicate-vat":
				f.Add("vat_dkk", "234.56")
			case "mismatch":
				f.Set("vat_dkk", "234.55")
			}
			if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices", f, b.site.URL); status != 400 {
				t.Fatalf("invalid new tax breakdown status=%d", status)
			}
		})
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_invoices`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid components created an invoice", err)
	}
}

// TestInvoiceTaxDatabaseConstraints exercises the restricted runtime role, not
// only the form parser; SQL NULL cannot silently bypass the exact-sum boundary.
func TestInvoiceTaxDatabaseConstraints(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	f.Set("net_dkk", "1000")
	f.Set("vat_dkk", "234.56")
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/invoices", f, b.site.URL); status != 303 {
		t.Fatalf("positive invoice control status=%d", status)
	}
	for i, tc := range []struct{ net, vat any }{
		{nil, nil}, {nil, int64(123456)}, {int64(123456), nil},
		{int64(-1), int64(123457)}, {int64(123457), int64(-1)},
		{int64(100000), int64(23455)}, {int64(1000000001), int64(0)},
		{int64(9223372036854775807), int64(1)},
	} {
		_, err := b.pool.Exec(t.Context(), `INSERT INTO portal_invoices(request_id,client_id,sequence,expected_sequence,submission_key,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,reference,purpose,currency,amount_minor,net_minor,vat_minor) SELECT request_id,client_id,2,1,$1,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,$2,purpose,currency,amount_minor,$3,$4 FROM portal_invoices WHERE sequence=1`, secret(), fmt.Sprintf("TAX-INVALID-%d", i), tc.net, tc.vat)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatal("database accepted invalid components or failed for another reason", err)
		}
	}
	for _, column := range []string{"net_minor", "vat_minor"} {
		if _, err := b.pool.Exec(t.Context(), `UPDATE portal_invoices SET `+column+`=0`); err == nil {
			t.Fatal("runtime mutated immutable tax metadata")
		}
	}
}

// TestInvoiceTaxMigrationRetainsLegacyReceipt preserves the historical amount,
// acceptance and retry identity without inventing components for old invoices.
func TestInvoiceTaxMigrationRetainsLegacyReceipt(t *testing.T) {
	b := invoiceFixture(t)
	f := invoiceForm(t, b)
	path := "/portal/operator/requests/" + proposalRequestID
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 303 {
		t.Fatal("fictional historical invoice setup failed")
	}
	m := migrationDB(t)
	rewindInvoiceTaxFixture(t, m)
	var before, after, ledger, retained string
	if err := m.QueryRow(t.Context(), `SELECT to_jsonb(i)::text FROM portal_invoices i`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=11`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(t.Context(), m); err != nil {
			t.Fatal("v12 could not retain the old invoice", err)
		}
	}
	if err := m.QueryRow(t.Context(), `SELECT (to_jsonb(i)-'net_minor'-'vat_minor')::text FROM portal_invoices i WHERE net_minor IS NULL AND vat_minor IS NULL`).Scan(&after); err != nil || before != after {
		t.Fatal("migration rewrote historical invoice facts", err)
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=11`).Scan(&retained); err != nil || ledger != retained {
		t.Fatal("migration changed the v1-v11 ledger", err)
	}
	legacy, err := url.ParseQuery(f.Encode())
	if err != nil {
		t.Fatal(err)
	}
	legacy.Del("net_dkk")
	legacy.Del("vat_dkk")
	for range 2 {
		if status, _, _ := b.request("POST", path+"/invoices", legacy, b.site.URL); status != 303 {
			t.Fatal("original legacy retry did not return its receipt")
		}
	}
	if status, _, _ := b.request("POST", path+"/invoices", f, b.site.URL); status != 409 {
		t.Fatal("retry invented tax components for a historical invoice")
	}
	saveFixtureProposal(t, b, proposalForm(t, b))
	if status, _, _ := b.request("POST", path+"/invoices", legacy, b.site.URL); status != 303 {
		t.Fatal("historical retry stopped working after re-quote")
	}
	b.reconnect(t.Context())
	for _, tc := range []struct{ lang, missing string }{
		{"en", "Tax breakdown not recorded"}, {"da", "Momsopdeling er ikke registreret"},
	} {
		status, body, _ := b.request("GET", path+"?lang="+tc.lang, nil, "")
		if status != 200 || !strings.Contains(body, tc.missing) || strings.Contains(body, "0.00 DKK") || strings.Contains(body, "0,00 DKK") {
			t.Fatal("historical unknown tax hidden or presented as zero")
		}
	}
	triageSignIn(t, b, "other-tax-operator", "operator-workspace", "operator")
	_, shell, _ := b.request("GET", "/portal/", nil, "")
	legacy.Set("csrf_token", html.UnescapeString(csrfInput.FindStringSubmatch(shell)[1]))
	if status, _, _ := b.request("POST", path+"/invoices", legacy, b.site.URL); status != 409 {
		t.Fatal("another operator reused a historical receipt")
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_invoices`).Scan(&count); err != nil || count != 1 {
		t.Fatal("historical retry duplicated or lost its receipt", err)
	}
}
