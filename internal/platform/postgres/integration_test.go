//go:build integration

package postgres

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/database/migrations"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres/dbsql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIntegrationFoundation(t *testing.T) {
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("integration requires explicit biho_live_test acknowledgment; never auto-select another target")
	}
	load := func(prefix string) config.DatabaseConfig {
		cfg, err := config.LoadDatabase(func(key string) (string, bool) { return os.LookupEnv(prefix + key) }, config.Test)
		if err != nil {
			t.Fatal("isolated test configuration missing or invalid")
		}
		if cfg.Name != "biho_live_test" {
			t.Fatal("integration may target only biho_live_test")
		}
		cfg.MaxConns = 2
		return cfg
	}
	app := load("TEST_")
	migration := load("TEST_MIGRATION_")
	if app.User != "biho_live_test_app" || migration.User != "biho_live_test_migrator" {
		t.Fatal("dedicated test roles required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	migrator, err := NewMigrator(migration)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := migrator.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Verify authenticated identity before version-table writes or DDL.
	var database, role string
	if err := migrator.db.QueryRowContext(ctx, "SELECT current_database(),current_user").Scan(&database, &role); err != nil || database != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("test identity verification failed")
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal("idempotent migration", err)
	}
	if v, err := migrator.Version(ctx); err != nil || v != 9 {
		t.Fatal("migration version")
	}
	if err := migrator.DownTest(ctx); err != nil {
		t.Fatal(err)
	}

	if v, err := migrator.Version(ctx); err != nil || v != 0 {
		t.Fatal("rollback version")
	}

	// Upgrade populated Step 8 data rather than resetting around the backfill.
	if _, err := migrator.provider.UpTo(ctx, 2); err != nil {
		t.Fatal("upgrade to identity", err)
	}
	fixtureID := "1234567890abcdef1234567890abcdef"
	if _, err := migrator.db.ExecContext(ctx, "INSERT INTO app.identity_accounts(id,display_name,avatar_url) VALUES($1,'বাংলা','https://example.invalid/old')", fixtureID); err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal("backfill upgrade", err)
	}
	var name, avatar string
	if err := migrator.db.QueryRowContext(ctx, "SELECT display_name,avatar_url FROM app.profiles WHERE account_id=$1", fixtureID).Scan(&name, &avatar); err != nil || name != "বাংলা" || avatar != "https://example.invalid/old" {
		t.Fatal("backfill lost profile")
	}
	if _, err := migrator.db.ExecContext(ctx, "UPDATE app.profiles SET display_name='' WHERE account_id=$1", fixtureID); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.provider.DownTo(ctx, 2); err != nil {
		t.Fatal("profile rollback", err)
	}
	if err := migrator.db.QueryRowContext(ctx, "SELECT display_name,avatar_url FROM app.identity_accounts WHERE id=$1", fixtureID).Scan(&name, &avatar); err != nil || name != "" || avatar != "https://example.invalid/old" {
		t.Fatal("rollback lost current profile")
	}
	if _, err := migrator.db.ExecContext(ctx, "DELETE FROM app.identity_accounts WHERE id=$1", fixtureID); err != nil {
		t.Fatal(err)
	}
	secondMigrator, err := NewMigrator(migration)
	if err != nil {
		t.Fatal(err)
	}
	upgrades := make(chan error, 2)
	go func() { upgrades <- migrator.Up(ctx) }()
	go func() { upgrades <- secondMigrator.Up(ctx) }()
	for range 2 {
		if err := <-upgrades; err != nil {
			t.Fatal("concurrent re-upgrade failed", err)
		}
	}
	if err := secondMigrator.Close(); err != nil {
		t.Fatal(err)
	}
	// A failed transactional upgrade must leave neither its DDL nor version 10.
	initial, err := fs.ReadFile(migrations.Source(), "00001_application_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	identityMigration, err := fs.ReadFile(migrations.Source(), "00002_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	profileMigration, err := fs.ReadFile(migrations.Source(), "00003_profiles_access.sql")
	if err != nil {
		t.Fatal(err)
	}
	walletMigration, err := fs.ReadFile(migrations.Source(), "00004_wallet_economy.sql")
	if err != nil {
		t.Fatal(err)
	}
	liveMigration, err := fs.ReadFile(migrations.Source(), "00005_live_agency.sql")
	if err != nil {
		t.Fatal(err)
	}
	policyMigration, err := fs.ReadFile(migrations.Source(), "00006_live_policy.sql")
	if err != nil {
		t.Fatal(err)
	}
	restrictionMigration, err := fs.ReadFile(migrations.Source(), "00007_live_restriction_history.sql")
	if err != nil {
		t.Fatal(err)
	}
	contextMigration, err := fs.ReadFile(migrations.Source(), "00008_agency_activity_contexts.sql")
	if err != nil {
		t.Fatal(err)
	}
	deliveryMigration, err := fs.ReadFile(migrations.Source(), "00009_delivery.sql")
	if err != nil {
		t.Fatal(err)
	}
	failingFS := fstest.MapFS{
		"00009_delivery.sql":                 &fstest.MapFile{Data: deliveryMigration},
		"00008_agency_activity_contexts.sql": &fstest.MapFile{Data: contextMigration},
		"00007_live_restriction_history.sql": &fstest.MapFile{Data: restrictionMigration},
		"00006_live_policy.sql":              &fstest.MapFile{Data: policyMigration},
		"00005_live_agency.sql":              &fstest.MapFile{Data: liveMigration},
		"00004_wallet_economy.sql":           &fstest.MapFile{Data: walletMigration},
		"00003_profiles_access.sql":          &fstest.MapFile{Data: profileMigration},
		"00001_application_schema.sql":       &fstest.MapFile{Data: initial},
		"00002_identity.sql":                 &fstest.MapFile{Data: identityMigration},
		"00010_failure.sql":                  &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE app.failed_upgrade_fixture(id bigint);\nSELECT 1/0;\n-- +goose Down\nDROP TABLE app.failed_upgrade_fixture;\n")},
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 10))
	if err != nil {
		t.Fatal(err)
	}
	faulty, err := goose.NewProvider(goose.DialectPostgres, migrator.db, failingFS, goose.WithTableName("migration.goose_db_version"), goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true), goose.WithLogger(quietMigrationLogger{}))
	if err != nil {
		t.Fatal("fault injection setup")
	}
	if _, err := faulty.Up(ctx); err == nil {
		t.Fatal("failed upgrade reported success")
	}
	var rolledBack bool
	if err := migrator.db.QueryRowContext(ctx, "SELECT to_regclass('app.failed_upgrade_fixture') IS NULL").Scan(&rolledBack); err != nil || !rolledBack {
		t.Fatal("failed upgrade retained DDL")
	}
	if v, err := migrator.Version(ctx); err != nil || v != 9 {
		t.Fatal("failed upgrade changed version")
	}

	pool, err := Open(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	identity, err := dbsql.New(pool).ConnectionIdentity(ctx)
	if err != nil || identity.DatabaseName != "biho_live_test" || identity.RoleName != "biho_live_test_app" {
		t.Fatal("generated identity query")
	}
	var encrypted bool
	var protocol string
	if err := pool.QueryRow(ctx, "SELECT ssl,version FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&encrypted, &protocol); err != nil || !encrypted {
		t.Fatal("TLS not active")
	}
	t.Log("TLS enabled; protocol", protocol, "; server identity is intentionally not verified in this testing environment")
	var elevated, createDB, temp, appCreate, appUsage, migrationUsage bool
	err = pool.QueryRow(ctx, `SELECT r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication,
 has_database_privilege(current_database(),'CREATE'),has_database_privilege(current_database(),'TEMP'),
 has_schema_privilege('app','CREATE'),has_schema_privilege('app','USAGE'),has_schema_privilege('migration','USAGE')
 FROM pg_roles r WHERE rolname=current_user`).Scan(&elevated, &createDB, &temp, &appCreate, &appUsage, &migrationUsage)
	if err != nil || elevated || createDB || temp || appCreate || !appUsage || migrationUsage {
		t.Fatal("runtime least-privilege policy")
	}
	_, err = pool.Exec(ctx, "CREATE TABLE app.forbidden_test_table(id bigint)")
	assertSQLState(t, err, "42501")
	_, err = pool.Exec(ctx, "SELECT version_id FROM migration.goose_db_version")
	assertSQLState(t, err, "42501")

	first, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("first acquisition")
	}
	second, err := pool.Acquire(ctx)
	if err != nil {
		first.Release()
		t.Fatal("second acquisition")
	}
	wait, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = pool.Acquire(wait)
	stop()
	first.Release()
	second.Release()
	if !errors.Is(err, context.DeadlineExceeded) || pool.Stat().TotalConns() > 2 {
		t.Fatal("pool bound/acquisition cancellation")
	}

	// Synchronize cancellation with server-observed query execution, not sleeps.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pid := conn.Conn().PgConn().PID()
	queryCtx, stopQuery := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, err := conn.Exec(queryCtx, "SELECT pg_sleep(30)"); result <- err }()
	observed := false
	for attempt := 0; attempt < 20; attempt++ {
		if err := pool.QueryRow(ctx, "SELECT COALESCE(wait_event = 'PgSleep',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&observed); err != nil {
			stopQuery()
			<-result
			conn.Release()
			t.Fatal("query observation failed")
		}
		if observed {
			break
		}
	}
	stopQuery()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "57014" {
				t.Error("in-flight cancellation not propagated")
			}
		}
	case <-ctx.Done():
		t.Error("query did not cancel")
	}
	conn.Release()
	if !observed {
		t.Fatal("server never observed cancellable query")
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("pool unusable after cancellation")
	}

	// Explicit migration-owned synthetic fixture; no production/business table.
	if _, err := migrator.db.ExecContext(ctx, "CREATE TABLE app.foundation_integration_fixture(id bigint PRIMARY KEY CHECK(id>0))"); err != nil {
		t.Fatal("fixture creation failed")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := migrator.db.ExecContext(cleanup, "DROP TABLE app.foundation_integration_fixture"); err != nil {
			t.Error("isolated fixture cleanup failed")
		}
	}()
	if _, err := migrator.db.ExecContext(ctx, "GRANT SELECT, INSERT ON app.foundation_integration_fixture TO biho_live_test_app"); err != nil {
		t.Fatal("fixture grant")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO app.foundation_integration_fixture(id) VALUES($1)", int64(1)); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal("parameterized insertion failed")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("rollback")
	}
	var count int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM app.foundation_integration_fixture").Scan(&count); err != nil || count != 0 {
		t.Fatal("rollback persisted data")
	}
	_, err = pool.Exec(ctx, "INSERT INTO app.foundation_integration_fixture(id) VALUES($1)", int64(-1))
	assertSQLState(t, err, "23514")
	var number int64
	if err := pool.QueryRow(ctx, "SELECT $1::bigint", int64(9007199254740993)).Scan(&number); err != nil || number != 9007199254740993 {
		t.Fatal("integer precision")
	}

	// A read-only login attempt confirms test credentials cannot enter dev.
	other := app
	other.Name = "biho_live_dev"
	other.Environment = config.Development // Intentional read-only dev login attempt; actual PostgreSQL ACL must also reject the test role.
	cc, err := ConnectionConfig(other)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized, err := pgx.ConnectConfig(ctx, cc)
	if unauthorized != nil {
		_ = unauthorized.Close(ctx)
		t.Fatal("test role could connect to dev")
	}
	assertSQLState(t, err, "42501")
}
func assertSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != want {
		t.Fatalf("expected SQLSTATE %s; raw diagnostics suppressed", want)
	}
}
