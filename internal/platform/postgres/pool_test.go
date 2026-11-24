package postgres

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/jackc/pgx/v5"
)

func testSettings(t *testing.T, environment config.Environment, changes map[string]string) config.DatabaseConfig {
	t.Helper()
	v := map[string]string{"DB_HOST": "127.0.0.1", "DB_PORT": "1", "DB_NAME": "biho_live_test", "DB_USER": "biho_live_test_app", "DB_PASSWORD": "PRIVATE_SENTINEL", "DB_SSLMODE": "require", "DB_CONNECT_TIMEOUT": "100ms"}
	for k, x := range changes {
		v[k] = x
	}
	cfg, err := config.LoadDatabase(func(key string) (string, bool) { x, ok := v[key]; return x, ok }, environment)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func TestConnectionSecurityAndTransactionPooling(t *testing.T) {
	for _, mode := range []string{"require", "verify-full"} {
		cfg := testSettings(t, config.Test, map[string]string{"DB_SSLMODE": mode})
		conn, err := ConnectionConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if conn.TLSConfig == nil || conn.TLSConfig.MinVersion != tls.VersionTLS12 || len(conn.Fallbacks) != 0 || conn.TLSConfig.InsecureSkipVerify != (mode == "require") {
			t.Fatal("TLS fallback or identity policy")
		}
		if conn.DefaultQueryExecMode != pgx.QueryExecModeExec || conn.StatementCacheCapacity != 0 || conn.DescriptionCacheCapacity != 0 || len(conn.RuntimeParams) != 1 {
			t.Fatal("session-dependent configuration")
		}
	}
	cfg := testSettings(t, config.Production, map[string]string{"DB_SSLMODE": "verify-full", "DB_NAME": "biho_live_prod", "DB_USER": "biho_live_prod_app"})
	cfg.SSLMode = "require"
	if _, err := ConnectionConfig(cfg); err == nil {
		t.Fatal("production mutation bypassed TLS")
	}
	cfg = testSettings(t, config.Test, map[string]string{"DB_SSLMODE": "verify-full", "DB_SSLROOTCERT": "/nonexistent/PRIVATE_SENTINEL.pem"})
	if _, err := ConnectionConfig(cfg); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe CA error")
	}
	cfg = testSettings(t, config.Test, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Open(ctx, cfg)
	if err == nil || pool != nil || strings.Contains(fmt.Sprint(err), "PRIVATE_SENTINEL") {
		t.Fatal("canceled pool startup")
	}
	cfg.User = "biho_live_test_migrator"
	if _, err := Open(context.Background(), cfg); err == nil {
		t.Fatal("runtime accepted migration role")
	}
	if _, err := NewMigrator(testSettings(t, config.Test, nil)); err == nil {
		t.Fatal("migration accepted runtime role")
	}
}
func TestDestructiveMigrationGuard(t *testing.T) {
	for _, name := range []string{"postgres", "biho_live_dev", "biho_live_prod"} {
		// Guard must fail before dereferencing provider/database connections.
		m := &Migrator{cfg: config.DatabaseConfig{Environment: config.Test, Name: name, User: name + "_migrator"}}
		if err := m.DownTest(context.Background()); err == nil {
			t.Fatal("destructive target accepted")
		}
	}
}
