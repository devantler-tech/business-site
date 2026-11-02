package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestProjectRequestOtherDurabilityAndClosedGates(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			b, form := clientRequestForm(t)
			form.Set("lang", lang)
			form.Set("product", "other")
			form.Set("title", "Fictional accessibility review")
			form.Set("brief", "Assess an existing project's accessibility. No real customer data.")
			label, assessment := "Other requests", "whether we can help"
			if lang == "da" {
				label, assessment = "Andre opgaver", "kan hjælpe"
			}
			status, body, _ := b.request("GET", "/portal/requests/new?lang="+lang, nil, "")
			if status != 200 || !strings.Contains(body, `value="other"`) || !strings.Contains(body, assessment) {
				t.Fatal("native form lacks the case-by-case category or assessment boundary")
			}
			status, _, headers := b.request("POST", "/portal/requests", form, b.site.URL)
			if status != 303 {
				t.Fatalf("other request submission=%d want 303", status)
			}
			location := headers.Get("Location")
			status, body, headers = b.request("GET", location, nil, "")
			if status != 200 || headers.Get("Cache-Control") != "no-store" || !strings.Contains(body, "<dd>"+label+"</dd>") || !strings.Contains(body, assessment) {
				t.Fatal("private receipt loses the category or case-by-case boundary")
			}
			var product, submitted string
			if err := b.pool.QueryRow(b.f.ctx, `SELECT product,submitted_at::text FROM portal_requests`).Scan(&product, &submitted); err != nil || product != "other" {
				t.Fatal("other request was not stored as its own category", err)
			}
			b.reconnect(context.Background())
			status, _, headers = b.request("POST", "/portal/requests", form, b.site.URL)
			if status != 303 || headers.Get("Location") != location {
				t.Fatal("reconnected retry did not retain the original receipt")
			}
			form.Set("product", "service")
			if status, _, _ = b.request("POST", "/portal/requests", form, b.site.URL); status != 409 {
				t.Fatal("a retry changed the original request category")
			}
			var rows, authority int
			var after string
			if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*),min(submitted_at)::text FROM portal_requests`).Scan(&rows, &after); err != nil || rows != 1 || after != submitted {
				t.Fatal("retry changed the durable request or submission time", err)
			}
			if err := b.pool.QueryRow(b.f.ctx, `SELECT (SELECT count(*) FROM portal_proposals)+(SELECT count(*) FROM portal_invoices)+(SELECT count(*) FROM portal_credit_transfers)+(SELECT count(*) FROM portal_work_approvals)`).Scan(&authority); err != nil || authority != 0 {
				t.Fatal("a request created a price, funding or work authority", err)
			}
			form.Set("product", "other")
			for _, adverse := range []string{"csrf", "origin"} {
				bad, err := url.ParseQuery(form.Encode())
				if err != nil {
					t.Fatal(err)
				}
				origin := b.site.URL
				if adverse == "csrf" {
					bad.Set("csrf_token", "invalid")
				} else {
					origin = "https://other.example.invalid"
				}
				if status, _, _ = b.request("POST", "/portal/requests", bad, origin); status != 403 {
					t.Fatal("other requests bypassed form-origin or CSRF protection")
				}
			}
			for _, value := range []any{nil, false, "true"} {
				requestFlag(t, b, value)
				if status, _, _ = b.request("POST", "/portal/requests", form, b.site.URL); status != 404 {
					t.Fatal("a closed request gate accepted the other category")
				}
			}
			requestFlag(t, b, true)
			if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "client-a"); err != nil {
				t.Fatal(err)
			}
			if status, _, _ = b.request("POST", "/portal/requests", form, b.site.URL); status != 403 {
				t.Fatal("revoked membership retained request authority")
			}
		})
	}
}

func TestOperatorInboxOtherRequestIsolationAndLabels(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			b, form := clientRequestForm(t)
			form.Set("product", "other")
			form.Set("lang", lang)
			status, _, headers := b.request("POST", "/portal/requests", form, b.site.URL)
			if status != 303 {
				t.Fatalf("other request submission=%d want 303", status)
			}
			location := headers.Get("Location")
			var id string
			if err := b.pool.QueryRow(b.f.ctx, `SELECT id FROM portal_requests`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			triageSignIn(t, b, "client-b", "client-b", "client")
			if status, _, _ = b.request("GET", location+"&client_id=client-a", nil, ""); status != 404 {
				t.Fatal("a second client read the other request")
			}
			triageFlags(t, b, true, true, true)
			for _, path := range []string{"/portal/operator/requests", "/portal/operator/requests/" + id} {
				if status, _, _ = b.request("GET", path+"?role=operator", nil, ""); status != 403 {
					t.Fatal("a client acquired operator presentation authority")
				}
			}
			triageSignIn(t, b, "operator", "operator-workspace", "operator")
			label := "Other requests"
			if lang == "da" {
				label = "Andre opgaver"
			}
			for _, path := range []string{"/portal/operator/requests", "/portal/operator/requests/" + id} {
				status, body, headers := b.request("GET", path+"?lang="+lang, nil, "")
				if status != 200 || headers.Get("Cache-Control") != "no-store" || !strings.Contains(body, label) {
					t.Fatal("operator list/detail loses the other category")
				}
			}
			if status, _, _ = b.request("GET", location, nil, ""); status != 403 {
				t.Fatal("an operator acquired client receipt authority")
			}
		})
	}
}

func TestRequestKindMigrationPreservesLegacyRequests(t *testing.T) {
	runtime := fixtureDB(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	if _, err := migrator.Exec(ctx, `ALTER TABLE portal_requests DROP CONSTRAINT portal_requests_product_check;
ALTER TABLE portal_requests ADD CONSTRAINT portal_requests_product_check CHECK(product IN ('website','app','service'));
DELETE FROM portal_schema_version WHERE version=15`); err != nil {
		t.Fatal("cannot prepare the disposable v14 request boundary", err)
	}
	var before string
	if err := migrator.QueryRow(ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=14`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i, product := range []string{"website", "app", "service"} {
		id := strings.Repeat(string(rune('A'+i)), 26)
		if _, err := runtime.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES($1,'legacy-client',$1,$2,'Existing project','Existing brief')`, id, product); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := Migrate(ctx, migrator); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	var versions, legacy int
	if err := migrator.QueryRow(ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=14`).Scan(&after); err != nil || before != after {
		t.Fatal("request-kind migration changed historical checksums", err)
	}
	if err := migrator.QueryRow(ctx, `SELECT count(*) FROM portal_schema_version`).Scan(&versions); err != nil || versions != 15 {
		t.Fatal("new request-kind migration is missing or not idempotent", err)
	}
	if err := runtime.QueryRow(ctx, `SELECT count(*) FROM portal_requests WHERE client_id='legacy-client' AND title='Existing project' AND brief='Existing brief' AND product IN ('website','app','service')`).Scan(&legacy); err != nil || legacy != 3 {
		t.Fatal("migration changed existing requests", err)
	}
	for i, product := range []string{"other", "unrecognized"} {
		id := strings.Repeat(string(rune('X'+i)), 26)
		_, err := runtime.Exec(ctx, `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief) VALUES($1,'new-client',$1,$2,'New project','New brief')`, id, product)
		var constraint *pgconn.PgError
		if (product == "other" && err != nil) || (product == "unrecognized" && (!errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "portal_requests_product_check")) {
			t.Fatal("database request-kind boundary differs", err)
		}
	}
}
