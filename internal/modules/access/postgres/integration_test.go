//go:build integration

package postgres

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"os"
	"sync"
	"testing"
	"time"
)

type googleFixture struct{}

func (googleFixture) Verify(_ context.Context, token, nonce string, _ time.Time) (identity.GoogleIdentity, error) {
	return identity.GoogleIdentity{Subject: token, DisplayName: "Synthetic", Email: "fixture@example.invalid", Avatar: "https://example.invalid/avatar"}, nil
}
func TestIntegrationProfilesAuthorization(t *testing.T) {
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated database acknowledgment required")
	}
	load := func(prefix, role string) config.DatabaseConfig {
		cfg, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
		if e != nil || cfg.Name != "biho_live_test" || cfg.User != role {
			t.Fatal("isolated credentials required")
		}
		cfg.MaxConns = 4
		return cfg
	}
	app := load("TEST_", "biho_live_test_app")
	migration := load("TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	connCfg, e := platform.ConnectionConfig(migration)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, connCfg)
	if e != nil {
		t.Fatal("operator connection")
	}
	defer operator.Close(context.Background())
	var database, role string
	if e := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&database, &role); e != nil || database != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("target guard")
	}
	var ownerCount int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.access_owners").Scan(&ownerCount); e != nil || ownerCount != 0 {
		t.Fatal("bootstrap fixture requires empty protected authority; never remove existing owner")
	}
	pool, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	other, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	ir, e := identitypg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	is, e := identity.New(ir, googleFixture{}, identity.NewPasswords())
	if e != nil {
		t.Fatal(e)
	}
	repo, e := New(pool)
	if e != nil {
		t.Fatal(e)
	}
	s, e := access.New(repo)
	if e != nil {
		t.Fatal(e)
	}
	repo2, e := New(other)
	if e != nil {
		t.Fatal(e)
	}
	s2, e := access.New(repo2)
	if e != nil {
		t.Fatal(e)
	}
	prefix := access.ID()
	var accounts, installs, subjects, roles []string
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for _, statement := range []string{
			"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])", "DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])", "DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_refresh_tokens WHERE session_id IN (SELECT id FROM app.identity_sessions WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_proofs WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])"} {
			if _, e := operator.Exec(cleanup, statement, accounts); e != nil {
				t.Error("tracked account cleanup failed")
			}
		}
		hashes := make([]string, 0, len(accounts))
		for _, id := range accounts {
			hashes = append(hashes, identity.Digest("profiles-access:"+id))
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=ANY($1::text[])", hashes); e != nil {
			t.Error("rate cleanup")
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.access_roles WHERE id=ANY($1::text[])", roles); e != nil {
			t.Error("role cleanup")
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_challenges WHERE installation_id=ANY($1::text[])", installs); e != nil {
			t.Error("challenge cleanup")
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_installations WHERE id=ANY($1::text[])", installs); e != nil {
			t.Error("installation cleanup")
		}
	}()
	signup := func() (string, access.Credentials) {
		t.Helper()
		install := access.ID()
		installs = append(installs, install)
		sub := prefix + install
		subjects = append(subjects, sub)
		c, e := is.Challenge(ctx, install, "login")
		if e != nil {
			t.Fatal(e)
		}
		out, e := is.Google(ctx, install, c.Token, sub)
		if e != nil {
			t.Fatal(e)
		}
		id := out.Tokens.AccountID
		accounts = append(accounts, id)
		return id, access.Credentials{Access: out.Tokens.Access, Installation: install}
	}
	owner, oc := signup()
	staff, sc := signup()
	target, tc := signup()
	var initialized int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.profiles WHERE account_id=ANY($1::text[])", accounts).Scan(&initialized); e != nil || initialized != 3 {
		t.Fatal("signup did not initialize exactly one profile each")
	}

	expect := func(err error, code apperror.Code) {
		t.Helper()
		if err == nil || apperror.Public(err).Code() != code {
			t.Fatalf("wanted %s got %v", code, err)
		}
	}
	effective, e := s.Permissions(ctx, oc)
	if e != nil || effective.Owner || len(effective.Permissions) != 0 {
		t.Fatal("implicit owner promotion")
	}
	_, e = s.Directory(ctx, oc, "", 20)
	expect(e, apperror.Forbidden)
	// Runtime credentials cannot grant protected owner, even by calling bootstrap adapter.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e := Bootstrap(ctx, tx, owner, "fixture", "bootstrap-runtime"); e == nil {
		t.Fatal("runtime granted owner")
	}
	if e := tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	bootstrap := func(conn *pgx.Conn, id string) error {
		transactions, e := platform.NewTransactions(conn, time.Second)
		if e != nil {
			return e
		}
		return transactions.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			return Bootstrap(ctx, tx, id, "isolated fixture", "bootstrap-fixture")
		})
	}
	// Invalid account and restricted/non-Google guards run without changing owners.
	if e := bootstrap(operator, access.ID()); e == nil {
		t.Fatal("missing bootstrap account")
	}
	if _, e := operator.Exec(ctx, "UPDATE app.identity_accounts SET status='restricted' WHERE id=$1", target); e != nil {
		t.Fatal(e)
	}
	if e := bootstrap(operator, target); e == nil {
		t.Fatal("restricted bootstrap")
	}
	if _, e := operator.Exec(ctx, "UPDATE app.identity_accounts SET status='active' WHERE id=$1", target); e != nil {
		t.Fatal(e)
	}
	nonGoogle := access.ID()
	if _, e := operator.Exec(ctx, "INSERT INTO app.identity_accounts(id) VALUES($1)", nonGoogle); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'','')", nonGoogle); e != nil {
		t.Fatal(e)
	}
	accounts = append(accounts, nonGoogle)
	if e := bootstrap(operator, nonGoogle); e == nil {
		t.Fatal("non-Google bootstrap")
	}
	op2, e := pgx.ConnectConfig(ctx, connCfg)
	if e != nil {
		t.Fatal(e)
	}
	defer op2.Close(context.Background())
	results := make(chan error, 2)
	go func() { results <- bootstrap(operator, owner) }()
	go func() { results <- bootstrap(op2, owner) }()
	success := 0
	for range 2 {
		if e := <-results; e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("concurrent bootstrap was not single winner")
	}
	effective, e = s2.Permissions(ctx, oc)
	if e != nil || !effective.Owner || len(effective.Permissions) != len(access.Catalog()) {
		t.Fatal("bootstrap effective permissions")
	}
	_, e = s.Profile(ctx, tc, owner)
	if e != nil {
		t.Fatal(e)
	}
	name, bio := "", "বাংলা\nprofile"
	if _, e := s.UpdateProfile(ctx, tc, "", profiles.Patch{DisplayName: &name, Bio: &bio}, "", ""); e != nil {
		t.Fatal(e)
	}
	c, e := is.Challenge(ctx, tc.Installation, "login")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := is.Google(ctx, tc.Installation, c.Token, subjects[2]); e != nil {
		t.Fatal(e)
	}
	p, e := s2.Profile(ctx, oc, target)
	if e != nil || p.DisplayName != "" || p.Bio != bio {
		t.Fatal("returning login overwrote edited/empty profile")
	}
	principal, e := is.Authenticate(ctx, tc.Access, tc.Installation)
	if e != nil {
		t.Fatal(e)
	}
	me, e := is.Current(ctx, principal)
	if e != nil || me.DisplayName != "" || me.Avatar != p.AvatarURL || me.Email != "fixture@example.invalid" {
		t.Fatal("identity contract composition")
	}
	// Concurrent partial edits preserve the other field on independent instances.
	name = "new"
	bio = "separate"
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() { _, e := s.UpdateProfile(ctx, tc, "", profiles.Patch{DisplayName: &name}, "", ""); errs <- e })
	wg.Go(func() { _, e := s2.UpdateProfile(ctx, tc, "", profiles.Patch{Bio: &bio}, "", ""); errs <- e })
	wg.Wait()
	for range 2 {
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
	}
	p, e = s.Profile(ctx, oc, target)
	if e != nil || p.DisplayName != name || p.Bio != bio {
		t.Fatal("partial update lost unrelated field")
	}
	// Module boundary failure rolls back signup account, binding and profile.
	rollbackID := access.ID()
	sentinel := errors.New("rollback fixture")
	e = ir.Within(ctx, func(st identity.Store) error {
		if e := st.CreateGoogle(ctx, identity.Account{ID: rollbackID, DisplayName: "fixture"}, prefix+"rollback"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal("signup rollback result")
	}
	var count int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.identity_accounts WHERE id=$1", rollbackID).Scan(&count); e != nil || count != 0 {
		t.Fatal("signup rollback leaked account")
	}
	roleRow, e := s.PutRole(ctx, oc, "", "support-"+prefix, []string{access.UsersRead, access.PrivateRead, access.ProfileUpdate, access.Restrict}, true, "fixture role", "role-create")
	if e != nil {
		t.Fatal(e)
	}
	roles = append(roles, roleRow.ID)
	if e := s.Assign(ctx, oc, staff, []string{roleRow.ID}, "fixture assignment", "assign-staff"); e != nil {
		t.Fatal(e)
	}
	_, e = s2.Private(ctx, sc, target)
	if e != nil {
		t.Fatal("staff private read", e)
	}
	_, e = s.PutRole(ctx, sc, "", "owner", []string{access.UsersRead}, true, "escalation", "escalate")
	expect(e, apperror.Forbidden)
	expect(s.Assign(ctx, sc, staff, []string{roleRow.ID}, "self", "self-assign"), apperror.Forbidden)
	expect(s.Assign(ctx, oc, owner, []string{roleRow.ID}, "self", "owner-assign"), apperror.Forbidden)
	expect(s.SetRestriction(ctx, sc, owner, true, "denied", "restrict-owner"), apperror.Forbidden)
	_, e = s.PutRole(ctx, oc, roleRow.ID, "owner", []string{"owner"}, false, "injection", "permission-inject")
	expect(e, apperror.ValidationFailed)
	// Audit failure after profile UPDATE must roll back the profile write.
	old := p.DisplayName
	changed := "must rollback"
	e = repo.Within(ctx, sc, access.ProfileUpdate, false, target, func(u access.Unit) error {
		if _, e := u.UpdateProfile(ctx, target, profiles.Patch{DisplayName: &changed}); e != nil {
			return e
		}
		return u.Audit(ctx, access.Audit{Actor: staff, Target: target, Action: "fixture", Fields: []string{"display_name"}, Reason: "fixture", Correlation: "invalid correlation"})
	})
	expect(e, apperror.ServiceUnavailable)
	p, e = s.Profile(ctx, oc, target)
	if e != nil || p.DisplayName != old {
		t.Fatal("audit failure committed profile")
	}
	// Permission revocation waits for an in-flight action, then applies across pools.
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	done := make(chan error, 1)
	go func() {
		done <- repo.Within(ctx, sc, access.ProfileUpdate, false, target, func(u access.Unit) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, e := u.UpdateProfile(ctx, target, profiles.Patch{DisplayName: &changed})
			if e != nil {
				return e
			}
			return u.Audit(ctx, access.Audit{Actor: staff, Target: target, Action: "fixture", Fields: []string{"display_name"}, Reason: "fixture", Correlation: "race-action"})
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("mutation failed to enter guarded transaction")
	}
	revoked := make(chan error, 1)
	go func() { revoked <- s2.Assign(ctx, oc, staff, []string{}, "revoke fixture", "revoke-race") }()
	select {
	case <-revoked:
		t.Fatal("revocation crossed mutation lock")
	case <-time.After(50 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-revoked; e != nil {
		t.Fatal(e)
	}
	_, e = s2.User(ctx, sc, target)
	expect(e, apperror.Forbidden)
	// Role definition revocation is also visible without reassignment or cache invalidation.
	if e := s.Assign(ctx, oc, staff, []string{roleRow.ID}, "restore", "role-revoke-setup"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PutRole(ctx, oc, roleRow.ID, "support-"+prefix, []string{}, false, "revoke permissions", "role-revoke"); e != nil {
		t.Fatal(e)
	}
	if _, e := s2.User(ctx, sc, target); apperror.Public(e).Code() != apperror.Forbidden {
		t.Fatal("role permission revocation stale")
	}
	// Restore restriction-only permission; private read requires users.read too.
	if _, e := s.PutRole(ctx, oc, roleRow.ID, "support-"+prefix, []string{access.Restrict}, false, "limited fixture", "limit-role"); e != nil {
		t.Fatal(e)
	}
	if e := s.Assign(ctx, oc, staff, []string{roleRow.ID}, "restore", "restore-assign"); e != nil {
		t.Fatal(e)
	}
	_, e = s2.Private(ctx, sc, target)
	expect(e, apperror.Forbidden)
	// Direct adapter calls cannot bypass permissions by choosing an empty guard.
	e = repo.Within(ctx, tc, "", false, owner, func(u access.Unit) error { return u.Restrict(ctx, owner, true) })
	expect(e, apperror.Forbidden)
	// Missing/failed audit rolls back restriction and affected sessions together.
	e = repo.Within(ctx, sc, access.Restrict, false, target, func(u access.Unit) error { return u.Restrict(ctx, target, true) })
	expect(e, apperror.ServiceUnavailable)
	e = repo.Within(ctx, sc, access.Restrict, false, target, func(u access.Unit) error {
		if e := u.Restrict(ctx, target, true); e != nil {
			return e
		}
		return u.Audit(ctx, access.Audit{Actor: staff, Target: target, Action: "fixture", Fields: []string{"restricted"}, Reason: "fixture", Correlation: "invalid correlation"})
	})
	expect(e, apperror.ServiceUnavailable)
	if _, e := is.Authenticate(ctx, tc.Access, tc.Installation); e != nil {
		t.Fatal("failed audit revoked session")
	}
	if v, e := s.User(ctx, oc, target); e != nil || v.Status != "active" {
		t.Fatal("failed audit retained restriction")
	}
	// An in-flight Identity session creation orders before restriction on the account lock.
	loginEntered, loginRelease := make(chan struct{}), make(chan struct{})
	var loginReleaseOnce sync.Once
	defer loginReleaseOnce.Do(func() { close(loginRelease) })
	loginDone := make(chan error, 1)
	freshAccess := access.ID() + "abcdefghijk"
	go func() {
		loginDone <- ir.Within(ctx, func(st identity.Store) error {
			if _, e := st.EnsureInstallation(ctx, tc.Installation); e != nil {
				return e
			}
			if _, e := st.LockAccount(ctx, target); e != nil {
				return e
			}
			close(loginEntered)
			select {
			case <-loginRelease:
			case <-ctx.Done():
				return ctx.Err()
			}
			return st.CreateSession(ctx, identity.Session{ID: access.ID(), Account: target, Installation: tc.Installation}, identity.Digest(freshAccess))
		})
	}()
	select {
	case <-loginEntered:
	case <-ctx.Done():
		t.Fatal("session race setup failed")
	}
	restrictionDone := make(chan error, 1)
	go func() {
		restrictionDone <- s2.SetRestriction(ctx, sc, target, true, "fixture restriction", "restrict-target")
	}()
	select {
	case <-restrictionDone:
		t.Fatal("restriction crossed account lock")
	case <-time.After(50 * time.Millisecond):
	}
	loginReleaseOnce.Do(func() { close(loginRelease) })
	if e := <-loginDone; e != nil {
		t.Fatal(e)
	}
	if e := <-restrictionDone; e != nil {
		t.Fatal(e)
	}
	if _, e := is.Authenticate(ctx, freshAccess, tc.Installation); e == nil {
		t.Fatal("restriction missed racing session")
	}

	if _, e := is.Authenticate(ctx, tc.Access, tc.Installation); e == nil {
		t.Fatal("restriction did not revoke session")
	}
	if e := s.SetRestriction(ctx, sc, target, false, "lift fixture", "lift-target"); e != nil {
		t.Fatal(e)
	}
	if _, e := is.Authenticate(ctx, tc.Access, tc.Installation); e == nil {
		t.Fatal("lifting revived session")
	}
	// Owner directory keyset returns deterministic bounded pages without duplicates.
	page, e := s.Directory(ctx, oc, "", 1)
	if e != nil || len(page.Users) != 1 || !page.More {
		t.Fatal("directory first page")
	}
	page2, e := s2.Directory(ctx, oc, page.Next, 1)
	if e != nil || len(page2.Users) != 1 || page.Users[0].AccountID >= page2.Users[0].AccountID {
		t.Fatal("directory cursor")
	}
	empty, e := s.Directory(ctx, oc, "ffffffffffffffffffffffffffffffff", 100)
	if e != nil || empty.Users == nil || len(empty.Users) != 0 || empty.More || empty.Next != "" {
		t.Fatal("empty directory semantics")
	}
	if e := s.DeleteRole(ctx, oc, roleRow.ID, "remove fixture", "delete-role"); e != nil {
		t.Fatal(e)
	}
	effective, e = s2.Permissions(ctx, sc)
	if e != nil || len(effective.Permissions) != 0 {
		t.Fatal("role deletion retained authority")
	}
	// Runtime cannot alter immutable audit history or protected authority.
	for _, sql := range []string{"UPDATE app.access_audit SET reason='tamper' WHERE actor=$1", "DELETE FROM app.access_audit WHERE actor=$1", "DELETE FROM app.access_owners WHERE account_id=$1"} {
		if _, e := pool.Exec(ctx, sql, owner); e == nil {
			t.Fatal("runtime immutable security grant")
		}
	}
	var leaked bool
	if e := operator.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM app.access_audit WHERE actor=ANY($1::text[]) AND fields && $2::text[])", accounts, []string{changed, "fixture@example.invalid", bio}).Scan(&leaked); e != nil || leaked {
		t.Fatal("audit contains private raw values")
	}
	t.Log("PASS: isolated bootstrap guards/concurrency, profile ownership/atomicity, two-pool revocation ordering, audit rollback, protected authority, directory and session restrictions")
	var auditCount int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.access_audit WHERE actor=ANY($1::text[])", accounts).Scan(&auditCount); e != nil || auditCount < 10 {
		t.Fatal("missing atomic audit evidence")
	}
	t.Log("PASS: isolated owner bootstrap guards/concurrency; profile ownership/partial edits; role and assignment revocation across pools; protected owner; restriction/session and audit rollback; bounded directory; tracked fixture cleanup")

	other.Close()
	if _, e := s2.Profile(ctx, sc, staff); apperror.Public(e).Code() != apperror.ServiceUnavailable {
		t.Fatal("security state loss did not fail closed")
	}

}
