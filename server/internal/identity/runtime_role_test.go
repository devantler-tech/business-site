package identity

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Changes live only inside a rolled-back fixture transaction, never in another
// package's runtime session. Real database/role names are refused before connect.
func TestRuntimeDatabasePrivileges(t *testing.T) {
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PORTAL_TEST_MIGRATION_DATABASE_URL"))
	if err != nil || cfg.ConnConfig.Database != "portal_identity_test" || cfg.ConnConfig.User != "portal_migrator" {
		t.Fatal("fresh fixture migration URL is required")
	}
	migrator, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err := Migrate(ctx, migrator); err != nil {
		t.Fatal(err)
	}
	ownerCfg := cfg.ConnConfig.Copy()
	ownerCfg.User = "portal_fixture_owner"
	owner, err := pgx.ConnectConfig(ctx, ownerCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Close(ctx); err != nil {
			t.Error("fixture owner connection cleanup failed")
		}
	}()
	type privilegeCase struct {
		name, setup string
		allowed     bool
	}
	cases := []privilegeCase{
		{"restricted", "SET ROLE portal_runtime", true},
		{"membership recorder disabled", "ALTER TABLE portal_memberships DISABLE TRIGGER portal_membership_transition; SET ROLE portal_runtime", false},
		{"membership incarnation disabled", "ALTER TABLE portal_memberships DISABLE TRIGGER portal_membership_incarnation; SET ROLE portal_runtime", false},
		{"membership recorder conditional", "DROP TRIGGER portal_membership_transition ON portal_memberships; CREATE TRIGGER portal_membership_transition AFTER INSERT OR UPDATE OR DELETE ON portal_memberships FOR EACH ROW WHEN (false) EXECUTE FUNCTION portal_record_membership_transition(); SET ROLE portal_runtime", false},
		{"membership recorder incomplete operations", "DROP TRIGGER portal_membership_transition ON portal_memberships; CREATE TRIGGER portal_membership_transition AFTER INSERT ON portal_memberships FOR EACH ROW EXECUTE FUNCTION portal_record_membership_transition(); SET ROLE portal_runtime", false},
		{"membership recorder search path", "ALTER FUNCTION portal_record_membership_transition() SET search_path=public,pg_temp; SET ROLE portal_runtime", false},
		{"membership TRUNCATE", "GRANT TRUNCATE ON portal_memberships TO portal_runtime; SET ROLE portal_runtime", false},
		{"membership TRIGGER", "GRANT TRIGGER ON portal_memberships TO portal_runtime; SET ROLE portal_runtime", false},
		{"membership MAINTAIN", "GRANT MAINTAIN ON portal_memberships TO portal_runtime; SET ROLE portal_runtime", false},
		{"trigger bypass", "GRANT SET ON PARAMETER session_replication_role TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence INSERT", "GRANT INSERT ON portal_membership_transitions TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence column INSERT", "GRANT INSERT(incarnation) ON portal_membership_transitions TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence UPDATE", "GRANT UPDATE ON portal_membership_transitions TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence DELETE", "GRANT DELETE ON portal_membership_transitions TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence TRUNCATE", "GRANT TRUNCATE ON portal_membership_transitions TO portal_runtime; SET ROLE portal_runtime", false},
		{"evidence SELECT delegation", "GRANT SELECT ON portal_membership_transitions TO portal_runtime WITH GRANT OPTION; SET ROLE portal_runtime", false},
		{"recorder EXECUTE", "GRANT EXECUTE ON FUNCTION portal_record_membership_transition() TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable evidence INSERT", "CREATE ROLE portal_fixture_membership_mutator NOLOGIN; GRANT INSERT ON portal_membership_transitions TO portal_fixture_membership_mutator; GRANT portal_fixture_membership_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable trigger bypass", "CREATE ROLE portal_fixture_membership_mutator NOLOGIN; GRANT SET ON PARAMETER session_replication_role TO portal_fixture_membership_mutator; GRANT portal_fixture_membership_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable recorder EXECUTE", "CREATE ROLE portal_fixture_membership_mutator NOLOGIN; GRANT EXECUTE ON FUNCTION portal_record_membership_transition() TO portal_fixture_membership_mutator; GRANT portal_fixture_membership_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"invoice column UPDATE", "GRANT UPDATE(amount_minor) ON portal_invoices TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable invoice column UPDATE", "CREATE ROLE portal_fixture_column_mutator NOLOGIN; GRANT UPDATE(reference) ON portal_invoices TO portal_fixture_column_mutator; GRANT portal_fixture_column_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"acceptance column UPDATE", "GRANT UPDATE(content_hash) ON portal_proposal_acceptances TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable acceptance column UPDATE", "CREATE ROLE portal_fixture_column_mutator NOLOGIN; GRANT UPDATE(content_hash) ON portal_proposal_acceptances TO portal_fixture_column_mutator; GRANT portal_fixture_column_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"visibility column UPDATE", "GRANT UPDATE(action) ON portal_proposal_visibility TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable visibility column UPDATE", "CREATE ROLE portal_fixture_column_mutator NOLOGIN; GRANT UPDATE(action) ON portal_proposal_visibility TO portal_fixture_column_mutator; GRANT portal_fixture_column_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"proposal column UPDATE", "GRANT UPDATE(scope) ON portal_proposals TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable proposal column UPDATE", "CREATE ROLE portal_fixture_column_mutator NOLOGIN; GRANT UPDATE(scope) ON portal_proposals TO portal_fixture_column_mutator; GRANT portal_fixture_column_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"request column UPDATE", "GRANT UPDATE(brief) ON portal_requests TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable request column UPDATE", "CREATE ROLE portal_fixture_column_mutator NOLOGIN; GRANT UPDATE(brief) ON portal_requests TO portal_fixture_column_mutator; GRANT portal_fixture_column_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"missing invoice SELECT", "REVOKE SELECT ON portal_invoices FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing invoice INSERT", "REVOKE INSERT ON portal_invoices FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable invoice UPDATE", "GRANT UPDATE ON portal_invoices TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable invoice DELETE", "GRANT DELETE ON portal_invoices TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable invoice TRUNCATE", "GRANT TRUNCATE ON portal_invoices TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable invoice UPDATE", "CREATE ROLE portal_fixture_invoice_mutator NOLOGIN; GRANT UPDATE ON portal_invoices TO portal_fixture_invoice_mutator; GRANT portal_fixture_invoice_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable invoice DELETE", "CREATE ROLE portal_fixture_invoice_mutator NOLOGIN; GRANT DELETE ON portal_invoices TO portal_fixture_invoice_mutator; GRANT portal_fixture_invoice_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable invoice TRUNCATE", "CREATE ROLE portal_fixture_invoice_mutator NOLOGIN; GRANT TRUNCATE ON portal_invoices TO portal_fixture_invoice_mutator; GRANT portal_fixture_invoice_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"consent domain owner", "GRANT CREATE ON SCHEMA public TO portal_runtime; ALTER DOMAIN portal_consent_key OWNER TO portal_runtime; REVOKE CREATE ON SCHEMA public FROM portal_runtime; SET ROLE portal_runtime", false},
		{"assumable consent domain owner", "CREATE ROLE portal_fixture_domain_owner NOLOGIN; GRANT CREATE ON SCHEMA public TO portal_fixture_domain_owner; ALTER DOMAIN portal_consent_hash OWNER TO portal_fixture_domain_owner; REVOKE CREATE ON SCHEMA public FROM portal_fixture_domain_owner; GRANT portal_fixture_domain_owner TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"mutable acceptance UPDATE", "GRANT UPDATE ON portal_proposal_acceptances TO portal_runtime; SET ROLE portal_runtime", false},
		{"missing acceptance SELECT", "REVOKE SELECT ON portal_proposal_acceptances FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing acceptance INSERT", "REVOKE INSERT ON portal_proposal_acceptances FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable acceptance DELETE", "GRANT DELETE ON portal_proposal_acceptances TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable acceptance TRUNCATE", "GRANT TRUNCATE ON portal_proposal_acceptances TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable acceptance UPDATE", "CREATE ROLE portal_fixture_consent_mutator NOLOGIN; GRANT UPDATE ON portal_proposal_acceptances TO portal_fixture_consent_mutator; GRANT portal_fixture_consent_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable acceptance DELETE", "CREATE ROLE portal_fixture_consent_mutator NOLOGIN; GRANT DELETE ON portal_proposal_acceptances TO portal_fixture_consent_mutator; GRANT portal_fixture_consent_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable acceptance TRUNCATE", "CREATE ROLE portal_fixture_consent_mutator NOLOGIN; GRANT TRUNCATE ON portal_proposal_acceptances TO portal_fixture_consent_mutator; GRANT portal_fixture_consent_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"missing review SELECT", "REVOKE SELECT ON portal_proposal_reviews FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing review INSERT", "REVOKE INSERT ON portal_proposal_reviews FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing review UPDATE", "REVOKE UPDATE ON portal_proposal_reviews FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable review DELETE", "GRANT DELETE ON portal_proposal_reviews TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable review TRUNCATE", "GRANT TRUNCATE ON portal_proposal_reviews TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable review DELETE", "CREATE ROLE portal_fixture_review_mutator NOLOGIN; GRANT DELETE ON portal_proposal_reviews TO portal_fixture_review_mutator; GRANT portal_fixture_review_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable review TRUNCATE", "CREATE ROLE portal_fixture_review_mutator NOLOGIN; GRANT TRUNCATE ON portal_proposal_reviews TO portal_fixture_review_mutator; GRANT portal_fixture_review_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"missing visibility SELECT", "REVOKE SELECT ON portal_proposal_visibility FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing visibility INSERT", "REVOKE INSERT ON portal_proposal_visibility FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable visibility UPDATE", "GRANT UPDATE ON portal_proposal_visibility TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable visibility DELETE", "GRANT DELETE ON portal_proposal_visibility TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable visibility TRUNCATE", "GRANT TRUNCATE ON portal_proposal_visibility TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable visibility UPDATE", "CREATE ROLE portal_fixture_visibility_mutator NOLOGIN; GRANT UPDATE ON portal_proposal_visibility TO portal_fixture_visibility_mutator; GRANT portal_fixture_visibility_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable visibility DELETE", "CREATE ROLE portal_fixture_visibility_mutator NOLOGIN; GRANT DELETE ON portal_proposal_visibility TO portal_fixture_visibility_mutator; GRANT portal_fixture_visibility_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable visibility TRUNCATE", "CREATE ROLE portal_fixture_visibility_mutator NOLOGIN; GRANT TRUNCATE ON portal_proposal_visibility TO portal_fixture_visibility_mutator; GRANT portal_fixture_visibility_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"missing proposal SELECT", "REVOKE SELECT ON portal_proposals FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing proposal INSERT", "REVOKE INSERT ON portal_proposals FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable proposal UPDATE", "GRANT UPDATE ON portal_proposals TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable proposal DELETE", "GRANT DELETE ON portal_proposals TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable proposal TRUNCATE", "GRANT TRUNCATE ON portal_proposals TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable proposal UPDATE", "CREATE ROLE portal_fixture_proposal_mutator NOLOGIN; GRANT UPDATE ON portal_proposals TO portal_fixture_proposal_mutator; GRANT portal_fixture_proposal_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable proposal DELETE", "CREATE ROLE portal_fixture_proposal_mutator NOLOGIN; GRANT DELETE ON portal_proposals TO portal_fixture_proposal_mutator; GRANT portal_fixture_proposal_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable proposal TRUNCATE", "CREATE ROLE portal_fixture_proposal_mutator NOLOGIN; GRANT TRUNCATE ON portal_proposals TO portal_fixture_proposal_mutator; GRANT portal_fixture_proposal_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"missing DELETE", "REVOKE DELETE ON portal_sessions FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing contact UPDATE", "REVOKE UPDATE ON portal_contacts FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing request INSERT", "REVOKE INSERT ON portal_requests FROM portal_runtime; SET ROLE portal_runtime", false},
		{"missing request SELECT", "REVOKE SELECT ON portal_requests FROM portal_runtime; SET ROLE portal_runtime", false},
		{"mutable request UPDATE", "GRANT UPDATE ON portal_requests TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable request DELETE", "GRANT DELETE ON portal_requests TO portal_runtime; SET ROLE portal_runtime", false},
		{"mutable request TRUNCATE", "GRANT TRUNCATE ON portal_requests TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable request UPDATE", "CREATE ROLE portal_fixture_request_mutator NOLOGIN; GRANT UPDATE ON portal_requests TO portal_fixture_request_mutator; GRANT portal_fixture_request_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable request DELETE", "CREATE ROLE portal_fixture_request_mutator NOLOGIN; GRANT DELETE ON portal_requests TO portal_fixture_request_mutator; GRANT portal_fixture_request_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable request TRUNCATE", "CREATE ROLE portal_fixture_request_mutator NOLOGIN; GRANT TRUNCATE ON portal_requests TO portal_fixture_request_mutator; GRANT portal_fixture_request_mutator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"only SELECT", "REVOKE INSERT,UPDATE,DELETE ON portal_login_attempts FROM portal_runtime; SET ROLE portal_runtime", false},
		{"schema creator", "GRANT CREATE ON SCHEMA public TO portal_runtime; SET ROLE portal_runtime", false},
		{"database creator", "GRANT CREATE ON DATABASE portal_identity_test TO portal_runtime; SET ROLE portal_runtime", false},
		{"assumable table owner", "CREATE ROLE portal_fixture_table_owner NOLOGIN; ALTER TABLE portal_sessions OWNER TO portal_fixture_table_owner; GRANT portal_fixture_table_owner TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable database creator", "CREATE ROLE portal_fixture_database_creator NOLOGIN; GRANT CREATE ON DATABASE portal_identity_test TO portal_fixture_database_creator; GRANT portal_fixture_database_creator TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"assumable database owner", "CREATE ROLE portal_fixture_database_owner NOLOGIN; ALTER DATABASE portal_identity_test OWNER TO portal_fixture_database_owner; GRANT portal_fixture_database_owner TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"server program executor", "GRANT pg_execute_server_program TO portal_runtime; SET ROLE portal_runtime", false},
		{"server file reader", "GRANT pg_read_server_files TO portal_runtime; SET ROLE portal_runtime", false},
		{"server file writer", "GRANT pg_write_server_files TO portal_runtime; SET ROLE portal_runtime", false},
		{"all data reader", "GRANT pg_read_all_data TO portal_runtime; SET ROLE portal_runtime", false},
		{"all data writer", "GRANT pg_write_all_data TO portal_runtime; SET ROLE portal_runtime", false},
		{"monitor", "GRANT pg_monitor TO portal_runtime; SET ROLE portal_runtime", false},
		{"replication", "ALTER ROLE portal_runtime REPLICATION; SET ROLE portal_runtime", false},
		{"assumable replication", "CREATE ROLE portal_fixture_replication NOLOGIN REPLICATION; GRANT portal_fixture_replication TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"indirect predefined privilege", "CREATE ROLE portal_fixture_privileged NOLOGIN; GRANT pg_execute_server_program TO portal_fixture_privileged; GRANT portal_fixture_privileged TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", false},
		{"fixture owner", "", false},
	}
	for _, history := range []struct{ table, column string }{
		{"portal_requests", "brief"}, {"portal_proposals", "scope"},
		{"portal_proposal_visibility", "action"}, {"portal_proposal_acceptances", "content_hash"},
		{"portal_invoices", "amount_minor"},
		{"portal_credit_transfers", "reference"}, {"portal_credit_revisions", "amount_minor"},
		{"portal_work_approvals", "evidence"}, {"portal_work_revocations", "reason"},
	} {
		for _, grant := range []struct{ privilege, option string }{
			{"TRIGGER", ""}, {"REFERENCES", ""}, {"REFERENCES(" + history.column + ")", ""}, {"MAINTAIN", ""},
			{"SELECT", " WITH GRANT OPTION"}, {"SELECT(" + history.column + ")", " WITH GRANT OPTION"},
			{"INSERT", " WITH GRANT OPTION"}, {"INSERT(" + history.column + ")", " WITH GRANT OPTION"},
		} {
			name := history.table + "/" + grant.privilege + grant.option
			cases = append(cases,
				privilegeCase{name, fmt.Sprintf("GRANT %s ON %s TO portal_runtime%s; SET ROLE portal_runtime", grant.privilege, history.table, grant.option), false},
				privilegeCase{"assumable/" + name, fmt.Sprintf("CREATE ROLE portal_fixture_history_guard NOLOGIN; GRANT %s ON %s TO portal_fixture_history_guard%s; GRANT portal_fixture_history_guard TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", grant.privilege, history.table, grant.option), false})
		}
		if history.table == "portal_credit_transfers" || history.table == "portal_credit_revisions" || history.table == "portal_work_approvals" || history.table == "portal_work_revocations" {
			for _, privilege := range []string{"SELECT", "INSERT"} {
				cases = append(cases, privilegeCase{history.table + "/missing " + privilege, fmt.Sprintf("REVOKE %s ON %s FROM portal_runtime; SET ROLE portal_runtime", privilege, history.table), false})
			}
			for _, privilege := range []string{"UPDATE", "UPDATE(" + history.column + ")", "DELETE", "TRUNCATE"} {
				cases = append(cases,
					privilegeCase{history.table + "/" + privilege, fmt.Sprintf("GRANT %s ON %s TO portal_runtime; SET ROLE portal_runtime", privilege, history.table), false},
					privilegeCase{"assumable/" + history.table + "/" + privilege, fmt.Sprintf("CREATE ROLE portal_fixture_credit_guard NOLOGIN; GRANT %s ON %s TO portal_fixture_credit_guard; GRANT portal_fixture_credit_guard TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", privilege, history.table), false})
			}
		}
	}
	for _, privilege := range []string{"UPDATE", "UPDATE(request_id)", "UPDATE(client_id)", "DELETE", "TRUNCATE", "TRIGGER", "REFERENCES", "MAINTAIN", "SELECT WITH GRANT OPTION", "INSERT WITH GRANT OPTION", "UPDATE(approval_id) WITH GRANT OPTION"} {
		grant := privilege
		option := ""
		const suffix = " WITH GRANT OPTION"
		if strings.HasSuffix(grant, suffix) {
			grant = strings.TrimSuffix(grant, suffix)
			option = suffix
		}
		cases = append(cases,
			privilegeCase{"work pointer/" + privilege, fmt.Sprintf("GRANT %s ON portal_work_current TO portal_runtime%s; SET ROLE portal_runtime", grant, option), false},
			privilegeCase{"assumable work pointer/" + privilege, fmt.Sprintf("CREATE ROLE portal_fixture_pointer_guard NOLOGIN; GRANT %s ON portal_work_current TO portal_fixture_pointer_guard%s; GRANT portal_fixture_pointer_guard TO portal_runtime WITH INHERIT FALSE; SET ROLE portal_runtime", grant, option), false})
	}
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE(approval_id)", "UPDATE(generation)"} {
		cases = append(cases, privilegeCase{"work pointer/missing " + privilege, fmt.Sprintf("REVOKE %s ON portal_work_current FROM portal_runtime; SET ROLE portal_runtime", privilege), false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := tx.Rollback(ctx); err != nil {
					t.Error("fixture privilege transaction cleanup failed")
				}
			}()
			// TRUNCATE is fixture reset authority, never deployed runtime authority.
			// Remove it within this rolled-back production-role control only.
			if _, err := tx.Exec(ctx, "REVOKE TRUNCATE ON portal_memberships FROM portal_runtime; GRANT SELECT ON portal_membership_transitions TO portal_runtime"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `GRANT SELECT,INSERT ON portal_work_approvals,portal_work_revocations,portal_work_current TO portal_runtime; REVOKE UPDATE,DELETE,TRUNCATE ON portal_work_approvals,portal_work_revocations,portal_work_current FROM portal_runtime; GRANT UPDATE(approval_id,generation) ON portal_work_current TO portal_runtime`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON portal_login_attempts,portal_invitations,portal_memberships,portal_sessions,portal_contacts TO portal_runtime; GRANT SELECT,INSERT ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions TO portal_runtime; REVOKE UPDATE,DELETE,TRUNCATE ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions FROM portal_runtime; GRANT SELECT,INSERT,UPDATE ON portal_proposal_reviews TO portal_runtime; REVOKE DELETE,TRUNCATE ON portal_proposal_reviews FROM portal_runtime"); err != nil {
				t.Fatal(err)
			}
			if tc.setup != "" {
				if _, err := tx.Exec(ctx, tc.setup); err != nil {
					t.Fatal(err)
				}
			}
			if err := CheckRuntimeRole(ctx, tx); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
