//go:build integration

package main

import (
	"context"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"os"
	"testing"
	"time"
)

// Read-only integration: no migrations, schema reset, fixture writes or server
// disruption. Only this test's pool/query is canceled and closed.
func TestIntegrationIdentitySchemaStartupBound(t *testing.T) {
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated database acknowledgment required")
	}
	cfg, err := config.LoadDatabase(func(key string) (string, bool) { return os.LookupEnv("TEST_" + key) }, config.Test)
	if err != nil || cfg.Name != "biho_live_test" || cfg.User != "biho_live_test_app" {
		t.Fatal("isolated test runtime configuration unavailable")
	}
	cfg.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal("isolated runtime connection unavailable")
	}
	defer pool.Close()
	repository, err := identitypg.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ready, stop := context.WithTimeout(ctx, cfg.ConnectTimeout)
	if err := repository.Ready(ready); err != nil {
		stop()
		t.Fatal("identity schema unavailable; apply migration 3 explicitly")
	}
	stop()
	closes := 0
	err = checkIdentitySchema(ctx, 100*time.Millisecond, func(check context.Context) error {
		// A bounded read represents a catalog query stalled on the wire/server.
		// No locks on application tables or other users' resources are taken.
		var slept any
		return pool.QueryRow(check, "SELECT pg_sleep(5)").Scan(&slept)
	}, func() { closes++; pool.Close() })
	if err == nil || closes != 1 {
		t.Fatal("stalled read escaped startup bound or cleanup")
	}
	if err := pool.Ping(ctx); err == nil {
		t.Fatal("failed initialization retained an open pool")
	}
	t.Log("PASS: isolated migration-3 schema read; timed read canceled; failed initialization closed only its own pool")
}
