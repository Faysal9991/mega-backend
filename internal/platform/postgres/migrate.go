package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Faysal9991/mega-backend/database/migrations"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Migrator owns a direct, single-connection database/sql bridge required by
// Goose. It never uses the runtime pgxpool or a transaction-pooling proxy.
type Migrator struct {
	db       *sql.DB
	provider *goose.Provider
	cfg      config.DatabaseConfig
}
type quietMigrationLogger struct{}

func (quietMigrationLogger) Printf(string, ...interface{}) {}
func (quietMigrationLogger) Fatalf(string, ...interface{}) {}

func NewMigrator(cfg config.DatabaseConfig) (*Migrator, error) {
	conn, err := ConnectionConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.User != cfg.Name+"_migrator" {
		return nil, errors.New("postgres: dedicated migration role is required")
	}
	db := stdlib.OpenDB(*conn)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 60), lock.WithUnlockTimeout(1, 5))
	if err != nil {
		_ = db.Close()
		return nil, errors.New("postgres: migration lock configuration failed")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Source(), goose.WithTableName("migration.goose_db_version"), goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true), goose.WithLogger(quietMigrationLogger{}))
	if err != nil {
		_ = db.Close()
		return nil, errors.New("postgres: migration provider initialization failed")
	}
	return &Migrator{db: db, provider: provider, cfg: cfg}, nil
}
func (m *Migrator) Close() error {
	if err := m.db.Close(); err != nil {
		return errors.New("postgres: migration close failed")
	}
	return nil
}
func (m *Migrator) Up(ctx context.Context) error {
	if _, err := m.provider.Up(ctx); err != nil {
		return errors.New("postgres: migration upgrade failed")
	}
	return nil
}
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	v, err := m.provider.GetDBVersion(ctx)
	if err != nil {
		return 0, errors.New("postgres: migration status failed")
	}
	return v, nil
}

// DownTest is intentionally unavailable for postgres/development/production.
// Verify actual authenticated identity before any version-table or DDL writes.
func (m *Migrator) DownTest(ctx context.Context) error {
	if m.cfg.Environment != config.Test || m.cfg.Name != "biho_live_test" || m.cfg.User != "biho_live_test_migrator" {
		return errors.New("postgres: destructive migration requires the isolated test target")
	}
	var database, role string
	if err := m.db.QueryRowContext(ctx, "SELECT current_database(), current_user").Scan(&database, &role); err != nil || database != "biho_live_test" || role != "biho_live_test_migrator" {
		return errors.New("postgres: test target identity verification failed")
	}
	if _, err := m.provider.DownTo(ctx, 0); err != nil {
		return errors.New("postgres: test migration rollback failed")
	}
	return nil
}
