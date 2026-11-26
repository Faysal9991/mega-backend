//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fixtureGoogle struct{}

func (fixtureGoogle) Verify(_ context.Context, token, nonce string, _ time.Time) (identity.GoogleIdentity, error) {
	if token == "invalid" || nonce == "" {
		return identity.GoogleIdentity{}, apperror.New(apperror.Unauthenticated, nil)
	}
	return identity.GoogleIdentity{Subject: token, DisplayName: "Synthetic provider name", Email: "synthetic@example.invalid", Avatar: "https://example.invalid/avatar"}, nil
}
func testConfig(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated test acknowledgment required")
	}
	cfg, err := config.LoadDatabase(func(key string) (string, bool) { return os.LookupEnv(prefix + key) }, config.Test)
	if err != nil || cfg.Name != "biho_live_test" || cfg.User != role {
		t.Fatal("dedicated isolated test configuration required")
	}
	cfg.MaxConns = 3
	return cfg
}
func TestIntegrationIdentitySecurity(t *testing.T) {
	app := testConfig(t, "TEST_", "biho_live_test_app")
	migration := testConfig(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	migrator, err := platform.NewMigrator(migration)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := migrator.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Up is explicitly authorized on this guarded isolated database, never runtime.
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := migrator.Version(ctx); err != nil || v != 12 {
		t.Fatal("identity migration version")
	}
	pool, err := platform.Open(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	otherPool, err := platform.Open(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	defer otherPool.Close()
	var database, role string
	if err := pool.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&database, &role); err != nil || database != "biho_live_test" || role != "biho_live_test_app" {
		t.Fatal("authenticated test target mismatch")
	}
	operatorConfig, err := platform.ConnectionConfig(migration)
	if err != nil {
		t.Fatal("isolated operator configuration")
	}
	operator, err := pgx.ConnectConfig(ctx, operatorConfig)
	if err != nil {
		t.Fatal("isolated operator connection")
	}
	defer operator.Close(context.Background())
	var operatorDB, operatorRole string
	if err := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&operatorDB, &operatorRole); err != nil || operatorDB != "biho_live_test" || operatorRole != "biho_live_test_migrator" {
		t.Fatal("isolated operator identity")
	}
	repo, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	repo2, err := New(otherPool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Ready(ctx); err != nil {
		t.Fatal("identity schema not ready")
	}
	passwords := identity.NewPasswords()
	service, err := identity.New(repo, fixtureGoogle{}, passwords)
	if err != nil {
		t.Fatal(err)
	}
	service2, err := identity.New(repo2, fixtureGoogle{}, passwords)
	if err != nil {
		t.Fatal(err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	installation := hex.EncodeToString(random[:])
	random[0] ^= 0xff
	installation2 := hex.EncodeToString(random[:])
	subject := "step8-" + installation
	subject2 := subject + "-other"
	var accountIDs []string
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		// Recover IDs from planned subjects even if a concurrent test failed after
		// commit but before recording its returned account ID.
		rows, e := pool.Query(cleanup, "SELECT account_id FROM app.identity_google WHERE subject=ANY($1::text[])", []string{subject, subject2})
		if e != nil {
			t.Error("tracked fixture lookup failed")
		} else {
			for rows.Next() {
				var id string
				if e := rows.Scan(&id); e != nil {
					t.Error("fixture scan failed")
				} else {
					accountIDs = append(accountIDs, id)
				}
			}
			if rows.Err() != nil {
				t.Error("fixture scan failed")
			}
			rows.Close()
		}
		// Only random tracked accounts/installations from this run, no global reset.
		if _, err := operator.Exec(cleanup, "DELETE FROM app.agency_activity_contexts WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])", accountIDs); err != nil {
			t.Error("tracked activity context cleanup failed")
		}
		if _, err := operator.Exec(cleanup, "DELETE FROM app.live_account_restrictions WHERE account_id=ANY($1::text[])", accountIDs); err != nil {
			t.Error("tracked restriction history cleanup failed")
		}
		for _, statement := range []string{
			"DELETE FROM app.identity_refresh_tokens WHERE session_id IN (SELECT id FROM app.identity_sessions WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_proofs WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_identifiers WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_passwords WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])",
		} {
			if _, err := pool.Exec(cleanup, statement, accountIDs); err != nil {
				t.Error("tracked identity fixture cleanup failed")
			}
		}
		for _, statement := range []string{"DELETE FROM app.identity_challenges WHERE installation_id=ANY($1::text[])", "DELETE FROM app.identity_installations WHERE id=ANY($1::text[])"} {
			if _, err := pool.Exec(cleanup, statement, []string{installation, installation2}); err != nil {
				t.Error("tracked installation cleanup failed")
			}
		}
	}()
	exchange := func(s *identity.Service, install, purpose, sub string) identity.GoogleResult {
		t.Helper()
		c, err := s.Challenge(ctx, install, purpose)
		if err != nil {
			t.Fatal("challenge failed", err)
		}
		out, err := s.Google(ctx, install, c.Token, sub)
		if err != nil {
			t.Fatal("fixture Google exchange failed", err)
		}
		return out
	}
	// Two instances and distinct challenges concurrently create exactly one account.
	c1, err := service.Challenge(ctx, installation, "login")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := service2.Challenge(ctx, installation2, "login")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan identity.GoogleResult, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for i, c := range []identity.ChallengeResult{c1, c2} {
		wg.Go(func() {
			s, inst := service, installation
			if i == 1 {
				s, inst = service2, installation2
			}
			v, e := s.Google(ctx, inst, c.Token, subject)
			results <- v
			failures <- e
		})
	}
	wg.Wait()
	for range 2 {
		if e := <-failures; e != nil {
			t.Fatal("concurrent signup failed", e)
		}
	}
	one, two := <-results, <-results
	if one.Tokens == nil || two.Tokens == nil || one.Tokens.AccountID != two.Tokens.AccountID {
		t.Fatal("concurrent signup duplicated account")
	}
	account := one.Tokens.AccountID
	accountIDs = append(accountIDs, account)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM app.identity_google WHERE subject=$1", subject).Scan(&count); err != nil || count != 1 {
		t.Fatal("provider uniqueness")
	}
	if _, err := service.Google(ctx, installation, c1.Token, subject); apperror.Public(err).Code() != apperror.Unauthenticated {
		t.Fatal("challenge replay accepted")
	}
	// Preserve EVERY existing profile field, including intentionally empty ones.
	if _, err := pool.Exec(ctx, "WITH updated AS (UPDATE app.identity_accounts SET contact_email='owner@example.invalid' WHERE id=$1) UPDATE app.profiles SET display_name='',avatar_url='' WHERE account_id=$1", account); err != nil {
		t.Fatal("profile fixture")
	}
	login := exchange(service, installation, "login", subject).Tokens
	meP, err := service2.Authenticate(ctx, login.Access, installation)
	if err != nil || meP.Account.DisplayName != "" || meP.Account.Email != "owner@example.invalid" || meP.Account.Avatar != "" {
		t.Fatal("returning Google overwrote profile")
	}
	if _, err := service.Authenticate(ctx, login.Access, installation2); err == nil {
		t.Fatal("installation binding bypass")
	}
	// No password or identifiers exist yet; no public/password account creation.
	unknown := "u" + installation[:12]
	if _, err := service.PasswordLogin(ctx, installation, unknown, "synthetic password value"); apperror.Public(err).Code() != apperror.Unauthenticated {
		t.Fatal("unknown password login unsafe")
	}
	proof := exchange(service, installation, "identifiers", subject).Proof
	phone := fmt.Sprintf("+99%012d", binary.BigEndian.Uint64(random[:8])%1000000000000) // synthetic canonical alias, never SMS ownership.
	if err := service.Identifiers(ctx, login.Access, installation, " U"+installation[:12]+" ", phone, proof); err != nil {
		t.Fatal("alias setup", err)
	}
	if _, err := service.PasswordLogin(ctx, installation, unknown, "synthetic password value"); apperror.Public(err).Code() != apperror.Unauthenticated {
		t.Fatal("passwordless account authenticated")
	}
	pwProof := exchange(service, installation, "password", subject).Proof
	password := "synthetic password value"
	if err := service.SetPassword(ctx, identity.PasswordChange{Installation: installation, Password: password, ProofToken: pwProof}); err != nil {
		t.Fatal("initial Google-bound password setup", err)
	}
	if _, err := service.Authenticate(ctx, login.Access, installation); err == nil {
		t.Fatal("password setup failed to revoke sessions")
	}
	if err := service.SetPassword(ctx, identity.PasswordChange{Installation: installation, Password: password, ProofToken: pwProof}); err == nil {
		t.Fatal("password proof reused")
	}
	logged, err := service.PasswordLogin(ctx, installation, unknown, password)
	if err != nil {
		t.Fatal("username login", err)
	}
	if _, err := service2.PasswordLogin(ctx, installation, phone, password); err != nil {
		t.Fatal("unverified alias login", err)
	}
	if _, err := service.PasswordLogin(ctx, installation, unknown, "incorrect password"); apperror.Public(err).Code() != apperror.Unauthenticated {
		t.Fatal("wrong password unsafe")
	}
	// Identifier collision is atomic: neither another alias nor proof is consumed.
	second := exchange(service, installation2, "login", subject2).Tokens
	accountIDs = append(accountIDs, second.AccountID)
	conflictProof := exchange(service, installation2, "identifiers", subject2).Proof
	if err := service.Identifiers(ctx, second.Access, installation2, "other_"+installation[:8], phone, conflictProof); apperror.Public(err).Code() != apperror.Conflict {
		t.Fatal("phone silently transferred", err)
	}
	if _, err := repo.FindIdentifier(ctx, "username", "other_"+installation[:8]); !errors.Is(err, identity.ErrMissing) {
		t.Fatal("alias conflict partially committed")
	}
	if err := service.Identifiers(ctx, second.Access, installation2, "other_"+installation[:8], "", conflictProof); err != nil {
		t.Fatal("conflict consumed proof despite rollback", err)
	}
	// The same email does not merge distinct Google subjects.
	if second.AccountID == account {
		t.Fatal("email-based linking")
	}
	// Reauthentication cannot create/link an unknown Google account.
	recoveryChallenge, e := service.Challenge(ctx, installation2, "password")
	if e != nil {
		t.Fatal(e)
	}
	unknownSubject := subject + "-unlinked"
	if _, e := service.Google(ctx, installation2, recoveryChallenge.Token, unknownSubject); apperror.Public(e).Code() != apperror.Unauthenticated {
		t.Fatal("reauthentication created an account")
	}
	if _, e := repo.GoogleAccount(ctx, unknownSubject); !errors.Is(e, identity.ErrMissing) {
		t.Fatal("unlinked recovery persisted account")
	}
	// Proofs cannot cross account, purpose or installation boundaries.
	otherProof := exchange(service, installation2, "identifiers", subject2).Proof
	if e := service.Identifiers(ctx, logged.Access, installation, "cross_account_alias", "", otherProof); e == nil {
		t.Fatal("cross-account proof accepted")
	}
	// Concurrent single-use refresh: one winner; reuse revokes the whole family.
	type refreshOutcome struct {
		tokens identity.Tokens
		err    error
	}
	rotated := make(chan refreshOutcome, 2)
	for _, s := range []*identity.Service{service, service2} {
		wg.Go(func() { v, e := s.Refresh(ctx, installation, logged.Refresh); rotated <- refreshOutcome{v, e} })
	}
	wg.Wait()
	successes := 0
	var winner identity.Tokens
	for range 2 {
		out := <-rotated
		if out.err == nil {
			successes++
			winner = out.tokens
		} else if apperror.Public(out.err).Code() != apperror.Unauthenticated {
			t.Fatal("refresh race storage failure", out.err)
		}
	}
	if winner.Access == "" {
		t.Fatal("refresh did not produce winner")
	}
	if successes != 1 {
		t.Fatal("refresh single-use race")
	}
	if _, err := service.Authenticate(ctx, winner.Access, installation); err == nil {
		t.Fatal("refresh reuse failed to revoke family")
	}
	// Current-password changes revoke sessions; Google proof recovers without one.
	logged, err = service.PasswordLogin(ctx, installation, unknown, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPassword(ctx, identity.PasswordChange{Installation: installation, Password: "new synthetic password", AccessToken: logged.Access, CurrentPassword: "wrong"}); err == nil {
		t.Fatal("wrong current password accepted")
	}
	if err := service.SetPassword(ctx, identity.PasswordChange{Installation: installation, Password: "new synthetic password", AccessToken: logged.Access, CurrentPassword: password}); err != nil {
		t.Fatal("password change", err)
	}
	if _, err := service2.Authenticate(ctx, logged.Access, installation); err == nil {
		t.Fatal("password change revocation")
	}
	recovery := exchange(service, installation, "password", subject).Proof
	if err := service.SetPassword(ctx, identity.PasswordChange{Installation: installation, Password: password, ProofToken: recovery}); err != nil {
		t.Fatal("Google recovery", err)
	}
	logged, err = service.PasswordLogin(ctx, installation, unknown, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, logged.Access, installation, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service2.Authenticate(ctx, logged.Access, installation); err == nil {
		t.Fatal("logout not visible on other instance")
	}
	logged, err = service.PasswordLogin(ctx, installation, unknown, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := service2.Logout(ctx, logged.Access, installation, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, logged.Access, installation); err == nil {
		t.Fatal("logout-all not visible")
	}
	// Account locking limits live families across instances; old access expires
	// immediately when the oldest family is pruned.
	var oldest *identity.Tokens
	for i := range 6 {
		s := service
		if i%2 == 1 {
			s = service2
		}
		out := exchange(s, installation, "login", subject).Tokens
		if i == 0 {
			oldest = out
		}
	}
	if _, e := service.Authenticate(ctx, oldest.Access, installation); e == nil {
		t.Fatal("session cap did not revoke oldest")
	}
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM app.identity_sessions WHERE account_id=$1 AND NOT revoked AND expires_at>clock_timestamp()", account).Scan(&count); e != nil || count != 5 {
		t.Fatal("live session cap")
	}
	// Restrictions apply on each protected request and every signup/recovery path.
	logged, err = service.PasswordLogin(ctx, installation, unknown, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo2.RestrictInstallation(ctx, installation); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, logged.Access, installation); err == nil {
		t.Fatal("installation ban bypass")
	}
	if _, err := service.Challenge(ctx, installation, "login"); err == nil {
		t.Fatal("restricted installation can create signup challenge")
	}
	if err := repo.RestrictAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	c, err := service.Challenge(ctx, installation2, "login")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Google(ctx, installation2, c.Token, subject); err == nil {
		t.Fatal("restricted account escaped via Google login")
	}
	// Actual database uniqueness/check/FK constraints, transaction rollback above.
	_, err = pool.Exec(ctx, "INSERT INTO app.identity_google(subject,account_id) VALUES($1,$2)", subject, second.AccountID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatal("Google uniqueness missing")
	}
	_, err = pool.Exec(ctx, "INSERT INTO app.identity_identifiers(account_id,kind,value) VALUES($1,'phone','017123')", second.AccountID)
	if !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatal("phone CHECK missing")
	}
	_, err = pool.Exec(ctx, "INSERT INTO app.identity_passwords(account_id,credential) VALUES($1,'synthetic')", installation)
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Fatal("account FK missing")
	}
	// Durable rate state is shared across repositories and cannot use Redis.
	rateKey := identity.Digest("fixture:" + installation)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, e := pool.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=$1", rateKey)
		if e != nil {
			t.Error("rate fixture cleanup")
		}
	}()
	n, err := repo.Rate(ctx, rateKey)
	if err != nil || n != 1 {
		t.Fatal("rate insert")
	}
	n, err = repo2.Rate(ctx, rateKey)
	if err != nil || n != 2 {
		t.Fatal("rate state not shared")
	}
	// Expired families can be reclaimed only after absolute expiry; active and
	// previously consumed refresh rows survive until then for reuse detection.
	oldFamily, e := repo.RefreshSession(ctx, identity.Digest(oldest.Refresh))
	if e != nil {
		t.Fatal("pruned refresh history lost before expiry")
	}
	if _, e := pool.Exec(ctx, "UPDATE app.identity_sessions SET expires_at=clock_timestamp()-interval '1 second',access_expires_at=clock_timestamp()-interval '2 seconds' WHERE id=$1 AND account_id=$2", oldFamily.ID, account); e != nil {
		t.Fatal("expiry fixture failed")
	}
	if e := repo.Cleanup(ctx); e != nil {
		t.Fatal("bounded security cleanup failed", e)
	}
	if _, e := repo.RefreshSession(ctx, identity.Digest(oldest.Refresh)); !errors.Is(e, identity.ErrMissing) {
		t.Fatal("expired refresh family retained")
	}
	// Local pool closure simulates security-state failure, never stops DB/Redis.
	otherPool.Close()
	if _, err := service2.Authenticate(ctx, second.Access, installation2); apperror.Public(err).Code() != apperror.ServiceUnavailable {
		t.Fatal("security-state failure opened access")
	}
	t.Log("PASS: migration 4, generated queries/constraints, two-instance signup/refresh/revocation, profiles, aliases, password setup/change/recovery, restrictions and safe DB failure; no Redis authority")
}
