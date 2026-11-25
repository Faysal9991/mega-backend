//go:build redis_integration

package main

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

// Only synthetic accounts in the acknowledged isolated database; no live Google exchange.
func step9RedisFixture(t *testing.T) (access.Credentials, string, func(bool)) {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("isolated DB acknowledgment required")
	}
	load := func(prefix, role string) config.DatabaseConfig {
		cfg, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
		if e != nil || cfg.Name != "biho_live_test" || cfg.User != role {
			t.Fatal("isolated fixture credentials required")
		}
		return cfg
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	pool, e := platform.Open(ctx, load("TEST_", "biho_live_test_app"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	cfg, e := platform.ConnectionConfig(load("TEST_MIGRATION_", "biho_live_test_migrator"))
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = operator.Close(context.Background()) })
	var db, role string
	if e := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("fixture connection identity")
	}
	var owners int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.access_owners").Scan(&owners); e != nil || owners != 0 {
		t.Fatal("fixture requires absent owner; never replace authority")
	}
	repo, e := identitypg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	var ids, installs []string
	var roleID string
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for _, sql := range []string{"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])", "DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])", "DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])"} {
			if _, e := operator.Exec(cleanup, sql, ids); e != nil {
				t.Error("tracked Redis-mode account cleanup")
			}
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.access_roles WHERE id=$1", roleID); e != nil {
			t.Error("role cleanup")
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_installations WHERE id=ANY($1::text[])", installs); e != nil {
			t.Error("installation cleanup")
		}
		hashes := []string{}
		for _, id := range ids {
			hashes = append(hashes, identity.Digest("profiles-access:"+id))
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=ANY($1::text[])", hashes); e != nil {
			t.Error("rate cleanup")
		}
	})
	seed := func() (string, access.Credentials) {
		id, install, token := access.ID(), access.ID(), access.ID()+"abcdefghijk"
		ids = append(ids, id)
		installs = append(installs, install)
		if e := repo.Within(ctx, func(s identity.Store) error {
			if _, e := s.EnsureInstallation(ctx, install); e != nil {
				return e
			}
			if e := s.CreateGoogle(ctx, identity.Account{ID: id, DisplayName: "Synthetic", Email: "private@example.invalid"}, id); e != nil {
				return e
			}
			return s.CreateSession(ctx, identity.Session{ID: access.ID(), Account: id, Installation: install}, identity.Digest(token))
		}); e != nil {
			t.Fatal(e)
		}
		return id, access.Credentials{Access: token, Installation: install}
	}
	owner, oc := seed()
	staff, sc := seed()
	txs, e := platform.NewTransactions(operator, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if e := txs.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		return accesspg.Bootstrap(ctx, tx, owner, "isolated Redis fixture", "redis-bootstrap")
	}); e != nil {
		t.Fatal(e)
	}
	ar, e := accesspg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	service, e := access.New(ar)
	if e != nil {
		t.Fatal(e)
	}
	r, e := service.PutRole(ctx, oc, "", "redis-fixture-"+owner, []string{access.UsersRead}, true, "fixture", "redis-role")
	if e != nil {
		t.Fatal(e)
	}
	roleID = r.ID
	assign := func(enabled bool) {
		t.Helper()
		roles := []string{}
		if enabled {
			roles = append(roles, roleID)
		}
		if e := service.Assign(ctx, oc, staff, roles, "fixture revocation", "redis-assignment"); e != nil {
			t.Fatal(e)
		}
	}
	assign(true)
	return sc, staff, assign
}
