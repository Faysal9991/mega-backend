//go:build integration

package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestIntegrationMigrationDiagnostics(t *testing.T) {
	cfg := followupSettings(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cc, e := ConnectionConfig(cfg)
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	c, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal(safeFailure("diagnostic connect", e))
	}
	defer c.Close(context.Background())
	var db, role string
	if e = c.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != cfg.Name || role != cfg.User {
		t.Fatal("authenticated diagnostic guard")
	}
	var sessions, waiting, blocked, max int
	var oldest float64
	e = c.QueryRow(ctx, `SELECT count(*)::int,count(*) FILTER(WHERE wait_event_type='Lock')::int,count(*) FILTER(WHERE cardinality(pg_blocking_pids(pid))>0)::int,COALESCE(max(extract(epoch FROM clock_timestamp()-xact_start)) FILTER(WHERE pid<>pg_backend_pid()),0)::float8 FROM pg_stat_activity WHERE datname=current_database()`).Scan(&sessions, &waiting, &blocked, &oldest)
	if e != nil {
		t.Fatal(safeFailure("diagnostic activity", e))
	}
	if e = c.QueryRow(ctx, "SELECT current_setting('max_connections')::int").Scan(&max); e != nil {
		t.Fatal(safeFailure("diagnostic limits", e))
	}
	t.Logf("guarded read-only diagnostics elapsed=%s database_sessions=%d lock_waiters=%d blocked_sessions=%d oldest_transaction_seconds=%.3f server_connection_limit=%d", time.Since(start), sessions, waiting, blocked, oldest, max)
}
