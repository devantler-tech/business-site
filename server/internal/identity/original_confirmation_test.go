package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type confirmationFailure struct{ err error }

func (q confirmationFailure) QueryRow(context.Context, string, ...any) pgx.Row { return q }
func (q confirmationFailure) Scan(...any) error                                { return q.err }

func TestOriginalConfirmationMissingIsDistinctFromStorageFailure(t *testing.T) {
	for _, failure := range []error{pgx.ErrNoRows, errors.New("private database diagnostic")} {
		got, err := readOriginalConfirmation(t.Context(), confirmationFailure{failure}, "request", "client", "acceptance", "order")
		if errors.Is(failure, pgx.ErrNoRows) {
			if err != nil || got.State != confirmationMissing || got.OutboxID != "" || got.SenderAcceptedAt != nil {
				t.Fatal("missing confirmation was not an explicit, empty result")
			}
		} else if err != errTriageStorage || got != (originalConfirmation{}) {
			t.Fatal("database failure leaked diagnostics or returned usable evidence")
		}
	}
}

func placeConfirmationOrder(t *testing.T, b *browserFixture, lang string) url.Values {
	t.Helper()
	form := orderForm(t, b, lang)
	status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", form, b.site.URL)
	if status != 303 {
		t.Fatalf("confirmation order refused: %d", status)
	}
	return form
}

// The actual HTTP producer and restricted database reader must agree on the
// original accepted agreement, independently of later mail attempts or callers.
func TestOriginalConfirmationBindsCompleteAgreementAndSenderAcceptance(t *testing.T) {
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			b := billingFixture(t)
			m := mailFixture(t, b)
			f := placeConfirmationOrder(t, b, lang)
			order := orderID(t, b)
			var client, acceptance, body string
			if err := b.pool.QueryRow(t.Context(), `SELECT o.client_id,o.acceptance_id,m.body FROM portal_orders o JOIN portal_mail_outbox m ON m.order_id=o.id WHERE o.id=$1`, order).Scan(&client, &acceptance, &body); err != nil {
				t.Fatal(err)
			}
			read := func() originalConfirmation {
				t.Helper()
				got, err := readOriginalConfirmation(t.Context(), b.pool, proposalRequestID, client, acceptance, order)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			prepared := read()
			digest := sha256.Sum256([]byte(body))
			if prepared.State != confirmationPrepared || prepared.OutboxID == "" || prepared.BodySHA256 != hex.EncodeToString(digest[:]) || prepared.TermsSHA256 != f.Get("terms_hash") || prepared.QueuedAt.IsZero() || prepared.SenderAcceptedAt != nil {
				t.Fatal("prepared confirmation lost its original complete producer binding")
			}
			for i := range 4 {
				keys := []string{proposalRequestID, client, acceptance, order}
				keys[i] = secret()
				got, err := readOriginalConfirmation(t.Context(), b.pool, keys[0], keys[1], keys[2], keys[3])
				if err != nil || got != (originalConfirmation{State: confirmationMissing}) {
					t.Fatal("a foreign request, client, acceptance or order received evidence")
				}
			}
			m.fail = true
			if err := b.svc.SendQueuedMail(t.Context()); err == nil || !reflect.DeepEqual(read(), prepared) {
				t.Fatal("failed send certified sender acceptance")
			}
			m.fail = false
			if err := b.svc.SendQueuedMail(t.Context()); err != nil {
				t.Fatal(err)
			}
			accepted := read()
			if accepted.State != confirmationSenderAccepted || accepted.SenderAcceptedAt == nil || accepted.SenderAcceptedAt.IsZero() || accepted.OutboxID != prepared.OutboxID || accepted.BodySHA256 != prepared.BodySHA256 || accepted.TermsSHA256 != prepared.TermsSHA256 || !accepted.QueuedAt.Equal(prepared.QueuedAt) {
				t.Fatal("sender acceptance lost the original immutable confirmation")
			}
			if err := b.svc.LinkOrderTerms(fixtureTermsEN+"\nChanged release", fixtureTermsDA+"\nChanged release"); err != nil {
				t.Fatal(err)
			}
			if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 || !reflect.DeepEqual(read(), accepted) {
				t.Fatal("retry replaced the frozen producer evidence")
			}
		})
	}
}

// Old mail, unknown future formats and damaged content cannot be upgraded by
// an old sender receipt. Only the producer can establish the complete format.
func TestOriginalConfirmationLegacyUnknownAndDamagedRemainMissing(t *testing.T) {
	b := billingFixture(t)
	placeConfirmationOrder(t, b, "en")
	order := orderID(t, b)
	var client, acceptance string
	if err := b.pool.QueryRow(t.Context(), `SELECT client_id,acceptance_id FROM portal_orders WHERE id=$1`, order).Scan(&client, &acceptance); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.SendQueuedMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	migrator := migrationDB(t)
	for _, mutation := range []string{
		`UPDATE portal_mail_outbox SET producer_format=NULL,acceptance_id=NULL,proposal_version=NULL,proposal_sha256=NULL,terms_sha256=NULL,body_sha256=NULL`,
		// Model an old reader seeing a later schema, without modifying the
		// production constraint or accepting an unknown format in this release.
		`ALTER TABLE portal_mail_outbox DROP CONSTRAINT portal_mail_outbox_producer_format_check, DROP CONSTRAINT portal_mail_complete_producer; UPDATE portal_mail_outbox SET producer_format=2`,
		`ALTER TABLE portal_mail_outbox DROP CONSTRAINT portal_mail_body_digest; UPDATE portal_mail_outbox SET body='Damaged frozen body'`,
	} {
		tx, err := migrator.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(t.Context(), mutation); err != nil {
			_ = tx.Rollback(t.Context())
			t.Fatal(err)
		}
		got, readErr := readOriginalConfirmation(t.Context(), tx, proposalRequestID, client, acceptance, order)
		rollbackErr := tx.Rollback(t.Context())
		if readErr != nil || rollbackErr != nil || got != (originalConfirmation{State: confirmationMissing}) {
			t.Fatal("unproven mail became eligible confirmation evidence", readErr, rollbackErr)
		}
	}
	if _, err := migrator.Exec(t.Context(), `REVOKE SELECT ON portal_mail_outbox FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	got, readErr := readOriginalConfirmation(t.Context(), b.pool, proposalRequestID, client, acceptance, order)
	if _, err := migrator.Exec(t.Context(), `GRANT SELECT ON portal_mail_outbox TO portal_runtime`); err != nil {
		t.Fatal(err)
	}
	if readErr != errTriageStorage || got != (originalConfirmation{}) {
		t.Fatal("a real restricted-read failure became usable or missing evidence")
	}
}

func TestOriginalConfirmationProducerConstraints(t *testing.T) {
	b := billingFixture(t)
	placeConfirmationOrder(t, b, "en")
	migrator := migrationDB(t)
	columns := []string{"producer_format", "acceptance_id", "proposal_version", "proposal_sha256", "terms_sha256", "body_sha256"}
	// Every partial NULL combination is refused, including ones SQL's
	// three-valued CHECK logic would otherwise admit. All NULL is legacy.
	for mask := 1; mask < (1<<len(columns))-1; mask++ {
		var assignments []string
		for i, name := range columns {
			if mask&(1<<i) != 0 {
				assignments = append(assignments, name+"=NULL")
			}
		}
		if _, err := migrator.Exec(t.Context(), `UPDATE portal_mail_outbox SET `+strings.Join(assignments, ",")); err == nil {
			t.Fatalf("partial producer evidence accepted at mask %d", mask)
		}
	}
	for _, assignment := range []string{
		"producer_format=2", "proposal_version=2", "acceptance_id='AAAAAAAAAAAAAAAAAAAAAAAAAA'",
		"proposal_sha256=repeat('0',64)", "terms_sha256='invalid'", "body_sha256=repeat('0',64)",
		"body='Changed agreement'", "kind='withdrawal-receipt'",
	} {
		if _, err := migrator.Exec(t.Context(), `UPDATE portal_mail_outbox SET `+assignment); err == nil {
			t.Fatal("invalid producer binding or body was accepted")
		}
	}
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_mail_outbox SET terms_sha256=repeat('0',64)`); err == nil {
		t.Fatal("runtime could rewrite the original producer evidence")
	}
}

func TestOriginalConfirmationMigrationKeepsLegacyUnproven(t *testing.T) {
	b := billingFixture(t)
	placeConfirmationOrder(t, b, "da")
	order := orderID(t, b)
	if err := b.svc.SendQueuedMail(t.Context()); err != nil {
		t.Fatal(err)
	}
	var client, acceptance, original, earlier string
	if err := b.pool.QueryRow(t.Context(), `SELECT o.client_id,o.acceptance_id,m.body FROM portal_orders o JOIN portal_mail_outbox m ON m.order_id=o.id WHERE o.id=$1`, order).Scan(&client, &acceptance, &original); err != nil {
		t.Fatal(err)
	}
	before, err := readOriginalConfirmation(t.Context(), b.pool, proposalRequestID, client, acceptance, order)
	if err != nil || before.State != confirmationSenderAccepted {
		t.Fatal("migration control did not begin with a complete accepted confirmation", err)
	}
	migrator := migrationDB(t)
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<30`).Scan(&earlier); err != nil {
		t.Fatal(err)
	}
	rewindOriginalConfirmationFixture(t, migrator)
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal(err)
		}
	}
	var retained bool
	if err := b.pool.QueryRow(t.Context(), `SELECT m.body=$2 AND m.queued_at=$3 AND m.producer_format IS NULL AND d.delivered_at=$4 AND (SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<30)=$5 FROM portal_mail_outbox m JOIN portal_mail_deliveries d ON d.outbox_id=m.id WHERE m.id=$1`, before.OutboxID, original, before.QueuedAt, before.SenderAcceptedAt, earlier).Scan(&retained); err != nil || !retained {
		t.Fatal("migration rewrote legacy agreement evidence or earlier schema", err)
	}
	got, err := readOriginalConfirmation(t.Context(), b.pool, proposalRequestID, client, acceptance, order)
	if err != nil || got != (originalConfirmation{State: confirmationMissing}) {
		t.Fatal("migration upgraded a legacy sender receipt into complete-agreement proof", err)
	}
}
