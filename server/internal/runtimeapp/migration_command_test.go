package runtimeapp

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationCommandFixture creates only a uniquely named disposable database on
// the already validated loopback fixture, then exercises verified PostgreSQL TLS.
func migrationCommandFixture(t *testing.T) (string, *pgxpool.Pool, *url.URL) {
	t.Helper()
	database := runtimeFixtureDatabase(t)
	adminURL := *database
	adminURL.Path = "/postgres"
	adminURL.User = url.UserPassword("portal_fixture_owner", "fixture-only")
	admin, err := pgx.Connect(t.Context(), adminURL.String())
	if err != nil {
		t.Fatal("disposable migration administrator unavailable")
	}
	name := fmt.Sprintf("portal_migration_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted+" OWNER portal_migrator"); err != nil {
		if err := admin.Close(t.Context()); err != nil {
			t.Error("migration administrator cleanup failed")
		}
		t.Fatal("disposable migration database creation failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error("owned migration database cleanup failed")
		}
		if err := admin.Close(ctx); err != nil {
			t.Error("migration administrator cleanup failed")
		}
	})
	database.Path = "/" + name
	database.User = url.UserPassword("portal_migrator", "fixture-only")
	pool, err := pgxpool.New(t.Context(), database.String())
	if err != nil {
		t.Fatal("disposable migration pool unavailable")
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(t.Context(), "REVOKE CREATE ON SCHEMA public FROM PUBLIC"); err != nil {
		t.Fatal("disposable schema authority preparation failed")
	}
	certificate := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(certificate.Close)
	database.Host = fixtureTLSDatabase(t, database.Host, certificate.TLS.Certificates[0])
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal("migration fixture CA write failed")
	}
	query := database.Query()
	query.Set("sslmode", "verify-full")
	query.Set("sslrootcert", ca)
	database.RawQuery = query.Encode()
	file := filepath.Join(dir, "migration-database")
	if err := os.WriteFile(file, []byte(database.String()), 0600); err != nil {
		t.Fatal("migration fixture private file write failed")
	}
	return file, pool, database
}

// buildMigrationCommand binds the actual command to a source receipt. Migration
// has no public snapshot, browser listener or federation dependency to assemble.
func buildMigrationCommand(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "business-site")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "-ldflags",
		"-X main.revision="+strings.Repeat("a", 40)+" -X main.publicDigest="+strings.Repeat("b", 64)+" -X main.buildMode=release", "./cmd/business-site")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("actual migration command compilation failed: %s", output)
	}
	return binary
}

// runMigrationCommand filters private ambient configuration and bounds the
// owned child. Assertions observe its exit/output and actual durable side effects.
func runMigrationCommand(t *testing.T, binary string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTAL_") && !strings.HasPrefix(value, "portal-") {
			command.Env = append(command.Env, value)
		}
	}
	// An unrelated, incomplete browser configuration must never be consulted.
	command.Env = append(command.Env, "PORTAL_OIDC_ISSUER=https://unused.example.invalid")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("migration command exceeded its fixture deadline")
	}
	return string(output), err
}

// Initial, repeated and overlapping actual jobs must use the embedded ordered
// transaction, preserve records and reject ledger drift rather than reset it.
func TestActualExecutableMigration(t *testing.T) {
	file, pool, _ := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	args := []string{"--migrate", "--migration-database-file", file}
	output, err := runMigrationCommand(t, binary, args...)
	if err != nil || output != "portal schema migration completed\n" {
		t.Fatal("actual command did not migrate the empty disposable database")
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM portal_schema_version").Scan(&count); err != nil || count != 6 {
		t.Fatal("actual command did not apply every embedded schema version")
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true)"); err != nil {
		t.Fatal("migration preservation fixture write failed")
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			if output, err := runMigrationCommand(t, binary, args...); err != nil || output != "portal schema migration completed\n" {
				t.Error("overlapping migration job did not safely repeat")
			}
		})
	}
	workers.Wait()
	var preserved bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM portal_memberships WHERE subject='existing' AND client_id='client-existing' AND revoked)").Scan(&preserved); err != nil || !preserved {
		t.Fatal("migration repeat changed durable authority")
	}
	var original string
	if err := pool.QueryRow(t.Context(), "SELECT checksum FROM portal_schema_version WHERE version=6").Scan(&original); err != nil {
		t.Fatal("original migration ledger unavailable")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE portal_schema_version SET checksum='changed' WHERE version=6"); err != nil {
		t.Fatal("migration drift fixture write failed")
	}
	if output, err := runMigrationCommand(t, binary, args...); err == nil || output != "portal schema migration failed\n" {
		t.Fatal("actual command accepted ledger drift or leaked private diagnostics")
	}
	if _, err := pool.Exec(t.Context(), "UPDATE portal_schema_version SET checksum=$1,version=99 WHERE version=6", original); err != nil {
		t.Fatal("unsupported version fixture write failed")
	}
	if output, err := runMigrationCommand(t, binary, args...); err == nil || output != "portal schema migration failed\n" {
		t.Fatal("actual command accepted an unsupported schema version")
	}
	var unchanged bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM portal_schema_version WHERE version=99 AND checksum=$1) AND EXISTS(SELECT 1 FROM portal_memberships WHERE subject='existing' AND revoked)", original).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("refused migration rewrote the ledger or existing record")
	}
}

// Actual jobs must refuse unsafe files, transport and privileges before any DDL.
// Reading the empty ledger independently catches an accidentally successful mode.
func TestActualMigrationRejectsUnsafeInputAndAuthority(t *testing.T) {
	file, pool, database := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	for _, tc := range []struct {
		name, connection string
		mode             os.FileMode
	}{
		{"runtime role", migrationFixtureRole(database, "portal_runtime"), 0600},
		{"administrator", migrationFixtureRole(database, "portal_fixture_owner"), 0600},
		{"plaintext", strings.Replace(database.String(), "sslmode=verify-full", "sslmode=disable", 1), 0600},
		{"unverified TLS", strings.Replace(database.String(), "sslmode=verify-full", "sslmode=require", 1), 0600},
		{"malformed", "not a database connection", 0600},
		{"public file", database.String(), 0644},
		{"oversized", strings.Repeat("x", 16385), 0600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(tc.connection), tc.mode); err != nil {
				t.Fatal("unsafe migration fixture write failed")
			}
			// WriteFile preserves an existing mode; set the exact control explicitly.
			if err := os.Chmod(file, tc.mode); err != nil {
				t.Fatal("migration fixture mode failed")
			}
			if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
				t.Fatal("unsafe migration input was accepted or private data escaped")
			}
			var absent bool
			if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
				t.Fatal("refused migration changed the empty schema")
			}
		})
	}
}

// migrationFixtureRole changes only a generated fixture URL, never ambient credentials.
func migrationFixtureRole(database *url.URL, role string) string {
	connectionURL := *database
	connectionURL.User = url.UserPassword(role, "fixture-only")
	return connectionURL.String()
}

// An occupied transaction lock must fail within the job's fixed lock budget,
// leave no partial ledger, and allow a later explicit retry once released.
func TestActualMigrationLockWaitIsBounded(t *testing.T) {
	file, pool, _ := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	lock, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal("migration lock fixture unavailable")
	}
	defer lock.Release()
	if _, err := lock.Exec(t.Context(), "SELECT pg_advisory_lock(1869820517)"); err != nil {
		t.Fatal("migration lock fixture failed")
	}
	started := time.Now()
	output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file)
	if err == nil || output != "portal schema migration failed\n" || time.Since(started) > 5*time.Second {
		t.Fatal("actual migration did not bound its occupied lock wait")
	}
	var absent bool
	if err := lock.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("timed-out migration left a partial ledger")
	}
	if _, err := lock.Exec(t.Context(), "SELECT pg_advisory_unlock(1869820517)"); err != nil {
		t.Fatal("migration lock fixture release failed")
	}
	if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err != nil || output != "portal schema migration completed\n" {
		t.Fatal("bounded lock failure prevented a later explicit migration")
	}
}

// Schema creation alone is insufficient: broad or assumable roles must not be
// allowed to run the job. Only this test's generated role is changed or removed.
func TestActualMigrationRejectsBroaderRoleGrants(t *testing.T) {
	file, pool, database := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	adminURL := runtimeFixtureDatabase(t)
	adminURL.Path = database.Path
	adminURL.User = url.UserPassword("portal_fixture_owner", "fixture-only")
	admin, err := pgx.Connect(t.Context(), adminURL.String())
	if err != nil {
		t.Fatal("migration authority fixture unavailable")
	}
	role := fmt.Sprintf("portal_migration_role_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT; GRANT CREATE ON SCHEMA public TO "+quoted); err != nil {
		if err := admin.Close(t.Context()); err != nil {
			t.Error("migration authority administrator cleanup failed")
		}
		t.Fatal("owned migration role creation failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP OWNED BY "+quoted+"; DROP ROLE "+quoted); err != nil {
			t.Error("owned migration role cleanup failed")
		}
		if err := admin.Close(ctx); err != nil {
			t.Error("migration authority administrator cleanup failed")
		}
	})
	if err := os.WriteFile(file, []byte(migrationFixtureRole(database, role)), 0600); err != nil {
		t.Fatal("migration role projection failed")
	}
	for _, tc := range []struct{ name, grant, revoke string }{
		{"superuser", "ALTER ROLE " + quoted + " SUPERUSER", "ALTER ROLE " + quoted + " NOSUPERUSER"},
		{"bypass", "ALTER ROLE " + quoted + " BYPASSRLS", "ALTER ROLE " + quoted + " NOBYPASSRLS"},
		{"database creation", "ALTER ROLE " + quoted + " CREATEDB", "ALTER ROLE " + quoted + " NOCREATEDB"},
		{"role creation", "ALTER ROLE " + quoted + " CREATEROLE", "ALTER ROLE " + quoted + " NOCREATEROLE"},
		{"replication", "ALTER ROLE " + quoted + " REPLICATION", "ALTER ROLE " + quoted + " NOREPLICATION"},
		{"assumable role", "GRANT pg_read_all_data TO " + quoted, "REVOKE pg_read_all_data FROM " + quoted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := admin.Exec(t.Context(), tc.grant); err != nil {
				t.Fatal("owned role authority control failed")
			}
			if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
				t.Error("broad migration authority was accepted or exposed")
			}
			if _, err := admin.Exec(t.Context(), tc.revoke); err != nil {
				t.Fatal("owned role authority restoration failed")
			}
			var absent bool
			if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
				t.Fatal("refused role modified the schema")
			}
		})
	}
}

// Request/process cancellation must beat the longer SQL lock bound and leave
// no partial migration, not merely return a cancelled context after committing.
func TestMigrationCancellationInterruptsActualLockWait(t *testing.T) {
	file, pool, _ := migrationCommandFixture(t)
	lock, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal("cancellation lock fixture unavailable")
	}
	defer lock.Release()
	if _, err := lock.Exec(t.Context(), "SELECT pg_advisory_lock(1869820517)"); err != nil {
		t.Fatal("cancellation lock fixture failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := MigrateDatabase(ctx, file); err != ErrMigration || time.Since(started) > 750*time.Millisecond {
		t.Fatal("migration job ignored operation cancellation")
	}
	var absent bool
	if err := lock.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("cancelled migration left a partial ledger")
	}
	if _, err := lock.Exec(t.Context(), "SELECT pg_advisory_unlock(1869820517)"); err != nil {
		t.Fatal("cancellation fixture unlock failed")
	}
}

// A partial DSN must not borrow a real target or credential from ambient PG
// settings. The control sets only this fixture's destination and verifies no DDL.
func TestMigrationRefusesAmbientAndOverriddenConnectionIdentity(t *testing.T) {
	file, pool, database := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	password, _ := database.User.Password()
	for key, value := range map[string]string{
		"PGHOST": database.Hostname(), "PGPORT": database.Port(), "PGUSER": database.User.Username(),
		"PGPASSWORD": password, "PGDATABASE": strings.TrimPrefix(database.Path, "/"),
		"PGSSLMODE": "verify-full", "PGSSLROOTCERT": database.Query().Get("sslrootcert"),
	} {
		t.Setenv(key, value)
	}
	for _, connection := range []string{"sslmode=verify-full", "postgres://" + database.Host + database.Path + "?" + database.RawQuery} {
		if err := os.WriteFile(file, []byte(connection), 0600); err != nil {
			t.Fatal("partial migration projection failed")
		}
		if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
			t.Fatal("partial connection borrowed ambient identity or target")
		}
		var absent bool
		if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
			t.Fatal("partial connection modified the schema")
		}
	}
	for _, key := range []string{"host", "port", "dbname", "user", "password", "options", "service"} {
		connectionURL := *database
		query := connectionURL.Query()
		query.Set(key, "unintended")
		connectionURL.RawQuery = query.Encode()
		if err := os.WriteFile(file, []byte(connectionURL.String()), 0600); err != nil {
			t.Fatal("overridden migration projection failed")
		}
		if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
			t.Fatal("query override changed migration identity or startup parameters")
		}
	}
}

// File parsing must reject a certificate pipe and ignore an ambient service
// pipe before either can defeat the job's operation deadline.
func TestMigrationBoundsTLSAndAmbientServiceFiles(t *testing.T) {
	for _, ambient := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambient-%t", ambient), func(t *testing.T) {
			file, pool, database := migrationCommandFixture(t)
			binary := buildMigrationCommand(t)
			pipe := filepath.Join(t.TempDir(), "unreadable.pipe")
			if err := syscall.Mkfifo(pipe, 0600); err != nil {
				t.Fatal("owned file-boundary fixture creation failed")
			}
			if ambient {
				t.Setenv("PGSERVICE", "unintended")
				t.Setenv("PGSERVICEFILE", pipe)
				if err := os.Chmod(database.Query().Get("sslrootcert"), 0644); err != nil {
					t.Fatal("public certificate permissions fixture failed")
				}
			} else {
				query := database.Query()
				query.Set("sslrootcert", pipe)
				database.RawQuery = query.Encode()
				if err := os.WriteFile(file, []byte(database.String()), 0600); err != nil {
					t.Fatal("owned certificate projection failed")
				}
			}
			started := time.Now()
			output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file)
			if time.Since(started) > 2*time.Second {
				t.Fatal("file parsing blocked before the database deadline")
			}
			if ambient {
				if err != nil || output != "portal schema migration completed\n" {
					t.Fatal("explicit connection inherited ambient service-file access")
				}
				var versions int
				if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM portal_schema_version").Scan(&versions); err != nil || versions != 6 {
					t.Fatal("explicit migration did not apply its six schema versions")
				}
			} else {
				if err == nil || output != "portal schema migration failed\n" {
					t.Fatal("non-regular certificate input was not refused privately")
				}
				var absent bool
				if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
					t.Fatal("refused certificate input modified the schema")
				}
			}
		})
	}
}

// Trust material has its own size boundary; ordinary public certificate
// permissions never relax the separate private connection-file requirement.
func TestMigrationCertificateSizeBoundary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "trust-material")
	for _, size := range []int{65536, 65537, 0} {
		content := strings.Repeat("a", size)
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal("certificate size fixture failed")
		}
		certificate, err := migrationCertificate(file)
		if size == 65536 {
			if err != nil || string(certificate) != content {
				t.Fatal("complete bounded public certificate bytes were changed")
			}
		} else if err != ErrMigration || certificate != nil {
			t.Fatal("empty or oversized certificate bytes were accepted")
		}
	}
	if err := os.WriteFile(file, []byte("trust material"), 0644); err != nil {
		t.Fatal("certificate integrity fixture failed")
	}
	if err := os.Chmod(file, 0666); err != nil {
		t.Fatal("writable certificate permissions fixture failed")
	}
	if certificate, err := migrationCertificate(file); err != ErrMigration || certificate != nil {
		t.Fatal("untrusted writable certificate material was accepted")
	}
}

// A protected leaf file cannot establish trust when a selected parent permits
// replacement. Observe the compiled job and independently verify no DDL.
func TestActualMigrationRejectsReplaceableInputPaths(t *testing.T) {
	for _, input := range []string{"certificate", "connection"} {
		t.Run(input, func(t *testing.T) {
			file, pool, database := migrationCommandFixture(t)
			binary := buildMigrationCommand(t)
			parent := filepath.Join(t.TempDir(), "replaceable")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal("owned path-boundary fixture creation failed")
			}
			if err := os.Chmod(parent, 0777); err != nil {
				t.Fatal("replaceable parent fixture permissions failed")
			}
			if input == "certificate" {
				certificate, err := os.ReadFile(database.Query().Get("sslrootcert"))
				if err != nil {
					t.Fatal("owned certificate fixture unavailable")
				}
				ca := filepath.Join(parent, "ca.pem")
				if err := os.WriteFile(ca, certificate, 0644); err != nil {
					t.Fatal("owned replaceable certificate fixture failed")
				}
				query := database.Query()
				query.Set("sslrootcert", ca)
				database.RawQuery = query.Encode()
			} else {
				file = filepath.Join(parent, "migration-database")
			}
			if err := os.WriteFile(file, []byte(database.String()), 0600); err != nil {
				t.Fatal("owned replaceable connection fixture failed")
			}
			if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
				t.Fatal("actual migration accepted a replaceable input path")
			}
			var absent bool
			if err := pool.QueryRow(t.Context(), "SELECT to_regclass('public.portal_schema_version') IS NULL").Scan(&absent); err != nil || !absent {
				t.Fatal("refused replaceable input path modified the schema")
			}
		})
	}
}

// The production projection shape remains supported for both inputs, while the
// command still operates only on this test's uniquely named disposable database.
func TestActualMigrationAcceptsConfinedProjection(t *testing.T) {
	_, pool, database := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	certificate, err := os.ReadFile(database.Query().Get("sslrootcert"))
	if err != nil {
		t.Fatal("owned projected certificate source unavailable")
	}
	dir := t.TempDir()
	version := filepath.Join(dir, "version")
	if err := os.Mkdir(version, 0700); err != nil {
		t.Fatal("owned projection generation failed")
	}
	if err := os.Symlink("version", filepath.Join(dir, "..data")); err != nil {
		t.Fatal("owned projection generation link failed")
	}
	for _, name := range []string{"ca.pem", "migration-database"} {
		if err := os.Symlink("..data/"+name, filepath.Join(dir, name)); err != nil {
			t.Fatal("owned projection entry link failed")
		}
	}
	if err := os.WriteFile(filepath.Join(version, "ca.pem"), certificate, 0644); err != nil {
		t.Fatal("owned projected trust material failed")
	}
	query := database.Query()
	query.Set("sslrootcert", filepath.Join(dir, "ca.pem"))
	database.RawQuery = query.Encode()
	if err := os.WriteFile(filepath.Join(version, "migration-database"), []byte(database.String()+"\n"), 0600); err != nil {
		t.Fatal("owned projected private connection failed")
	}
	if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", filepath.Join(dir, "migration-database")); err != nil || output != "portal schema migration completed\n" {
		t.Fatal("actual job refused trusted confined projected inputs")
	}
	var versions int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM portal_schema_version").Scan(&versions); err != nil || versions != 6 {
		t.Fatal("actual projected-input migration did not apply every version")
	}
}

// A current ledger is not permission to report success with a non-owning role.
// Each control changes only its uniquely named, fully migrated disposable store.
func TestActualMigrationRequiresExistingRelationOwnership(t *testing.T) {
	for _, relation := range []string{"wrong credential", "portal_schema_version", "portal_login_attempts", "portal_invitations", "portal_memberships", "portal_sessions", "portal_contacts", "portal_requests", "portal_proposals"} {
		t.Run(relation, func(t *testing.T) {
			file, pool, database := migrationCommandFixture(t)
			binary := buildMigrationCommand(t)
			if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err != nil || output != "portal schema migration completed\n" {
				t.Fatal("owned complete-schema fixture migration failed")
			}
			if _, err := pool.Exec(t.Context(), "INSERT INTO portal_memberships VALUES('https://fixture.invalid','retained','client-retained','client',true)"); err != nil {
				t.Fatal("owned schema-preservation fixture failed")
			}
			adminURL := runtimeFixtureDatabase(t)
			adminURL.Path = database.Path
			adminURL.User = url.UserPassword("portal_fixture_owner", "fixture-only")
			admin, err := pgx.Connect(t.Context(), adminURL.String())
			if err != nil {
				t.Fatal("owned authority-fixture administrator unavailable")
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := admin.Close(ctx); err != nil {
					t.Error("owned authority administrator cleanup failed")
				}
			})
			connection := database.String()
			if relation == "wrong credential" {
				role := fmt.Sprintf("portal_migration_reader_%d_%d", os.Getpid(), time.Now().UnixNano())
				quoted := pgx.Identifier{role}.Sanitize()
				if _, err := admin.Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT; GRANT USAGE,CREATE ON SCHEMA public TO "+quoted+"; GRANT SELECT ON ALL TABLES IN SCHEMA public TO "+quoted); err != nil {
					t.Fatal("owned wrong-credential role fixture failed")
				}
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, err := admin.Exec(ctx, "DROP OWNED BY "+quoted+"; DROP ROLE "+quoted); err != nil {
						t.Error("owned wrong-credential role cleanup failed")
					}
				})
				connection = migrationFixtureRole(database, role)
			} else {
				quoted := pgx.Identifier{"public", relation}.Sanitize()
				if _, err := admin.Exec(t.Context(), "ALTER TABLE "+quoted+" OWNER TO portal_fixture_owner; GRANT SELECT ON ALL TABLES IN SCHEMA public TO portal_migrator"); err != nil {
					t.Fatal("owned partial-ownership fixture failed")
				}
			}
			if err := os.WriteFile(file, []byte(connection), 0600); err != nil {
				t.Fatal("owned migration-authority projection failed")
			}
			if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err == nil || output != "portal schema migration failed\n" {
				t.Error("actual job reported success without owning the managed schema")
			}
			var preserved bool
			if err := admin.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM portal_schema_version)=6 AND EXISTS(SELECT 1 FROM portal_memberships WHERE subject='retained' AND revoked)").Scan(&preserved); err != nil || !preserved {
				t.Fatal("ownership refusal changed the ledger or retained record")
			}
		})
	}
}
