//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type financeGoogle struct{}

func (financeGoogle) Verify(_ context.Context, subject, nonce string, _ time.Time) (identity.GoogleIdentity, error) {
	if subject == "" || nonce == "" {
		return identity.GoogleIdentity{}, identity.ErrMissing
	}
	return identity.GoogleIdentity{Subject: subject, DisplayName: "Synthetic financial fixture", Email: "financial-fixture@example.invalid"}, nil
}

type verifiedActivityFixture struct {
	days, minutes int
	before        func()
}

func (s verifiedActivityFixture) Sessions(_ context.Context, account string, start, end time.Time) ([]agency.Session, error) {
	if s.before != nil {
		s.before()
	}
	sessions := make([]agency.Session, 0, s.days)
	for i := 0; i < s.days; i++ {
		at := start.AddDate(0, 0, i).Add(10 * time.Hour)
		if !at.Before(end) {
			break
		}
		sessions = append(sessions, agency.Session{ID: fmt.Sprintf("verified-fixture-%d", i), AccountID: account, Start: at, End: at.Add(time.Duration(s.minutes) * time.Minute)})
	}
	return sessions, nil
}
func financeConfig(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated financial test acknowledgment required")
	}
	cfg, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
	if e != nil || cfg.Name != "biho_live_test" || cfg.User != role {
		t.Fatal("dedicated isolated financial test configuration required")
	}
	cfg.MaxConns = 5
	return cfg
}
func decodeFinance[T any](t *testing.T, value any) T {
	t.Helper()
	var out T
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}

// This wrapper injects an error only after the real transaction commits. It
// models an ambiguous response boundary without lying about a rolled-back DB.
type ambiguousCommitGuard struct {
	delegate FinancialGuard
	calls    int
}

func (g *ambiguousCommitGuard) Financial(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	g.calls++
	if e := g.delegate.Financial(ctx, c, p, owner, target, fn); e != nil {
		return e
	}
	return errors.New("synthetic response lost after commit")
}

// diagnosticFinancialGuard reports only SQLSTATE/public PostgreSQL messages
// from synthetic fixture callbacks. Never print the underlying private error,
// connection settings, SQL arguments or PostgreSQL Detail/Where fields.
type diagnosticFinancialGuard struct {
	delegate FinancialGuard
	t        *testing.T
}

func (g diagnosticFinancialGuard) Financial(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	entered, callbackSucceeded := false, false
	err := g.delegate.Financial(ctx, c, p, owner, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		entered = true
		callbackErr := fn(ctx, tx, u)
		callbackSucceeded = callbackErr == nil
		var pgErr *pgconn.PgError
		if errors.As(callbackErr, &pgErr) {
			g.t.Logf("financial callback PostgreSQL SQLSTATE=%s message=%s", pgErr.Code, pgErr.Message)
		}
		return callbackErr
	})
	if err != nil {
		g.t.Logf("financial failure phase: callback_entered=%t callback_succeeded=%t safe_code=%s", entered, callbackSucceeded, apperror.Public(err).Code())
	}
	return err
}

type unavailableAuditUnit struct{ access.Unit }

func (unavailableAuditUnit) Audit(context.Context, access.Audit) error {
	return errors.New("synthetic audit persistence unavailable")
}

type unavailableAuditGuard struct{ delegate FinancialGuard }

func (g unavailableAuditGuard) Financial(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	return g.delegate.Financial(ctx, c, p, owner, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error { return fn(ctx, tx, unavailableAuditUnit{u}) })
}

func TestIntegrationFinancialEngine(t *testing.T) {
	app := financeConfig(t, "TEST_", "biho_live_test_app")
	migration := financeConfig(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cfg, e := platform.ConnectionConfig(migration)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatal("isolated migration-role connection failed")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("financial fixture target guard")
	}
	var existingOwners int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.access_owners").Scan(&existingOwners); e != nil || existingOwners != 0 {
		t.Fatal("financial bootstrap fixture requires no existing protected authority; never replace an owner")
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
	repo, e := New(pool, agency.UnavailableSource{})
	if e != nil {
		t.Fatal(e)
	}
	if e := repo.Ready(ctx); e != nil {
		t.Fatal("explicit financial migration required before tests")
	}
	repo2, e := New(other, agency.UnavailableSource{})
	if e != nil {
		t.Fatal(e)
	}
	engine, second := wallet.New(repo), wallet.New(repo2)
	ir, e := identitypg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	auth, e := identity.New(ir, financeGoogle{}, identity.NewPasswords())
	if e != nil {
		t.Fatal(e)
	}
	nonce := access.ID()
	var accountIDs, installationIDs, policyIDs, roleIDs []string
	// Migrator-only teardown reverses ONLY this run's tracked financial postings
	// before deleting fixtures; retained platform accounts remain reconciled.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 60*time.Second)
		defer stop()
		tx, e := operator.Begin(cleanup)
		if e != nil {
			t.Error("financial cleanup begin")
			return
		}
		defer tx.Rollback(cleanup)
		statements := []string{
			"UPDATE app.wallet_accounts a SET balance=(a.balance::numeric-d.delta)::bigint FROM (SELECT e.account_id,sum(e.delta::numeric) AS delta FROM app.wallet_entries e JOIN app.wallet_operations o ON o.id=e.operation_id WHERE o.actor_id=ANY($1::text[]) OR o.target_id=ANY($1::text[]) GROUP BY e.account_id) d WHERE a.id=d.account_id",
			"DELETE FROM app.wallet_withdrawal_events WHERE actor_id=ANY($1::text[]) OR withdrawal_id IN (SELECT id FROM app.wallet_withdrawals WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.wallet_withdrawals WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.wallet_entries WHERE operation_id IN (SELECT id FROM app.wallet_operations WHERE actor_id=ANY($1::text[]) OR target_id=ANY($1::text[]))",
			"DELETE FROM app.wallet_operations WHERE actor_id=ANY($1::text[]) OR target_id=ANY($1::text[])",
			"DELETE FROM app.economy_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.agency_host_associations WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])",
			"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])",
			"DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_refresh_tokens WHERE session_id IN (SELECT id FROM app.identity_sessions WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_proofs WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_identifiers WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_passwords WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])",
		}
		for _, sql := range statements {
			if _, e := tx.Exec(cleanup, sql, accountIDs); e != nil {
				t.Error("tracked financial fixture cleanup failed")
				return
			}
		}
		// Policy self-references require newest-first removal, not a global reset.
		for i := len(policyIDs) - 1; i >= 0; i-- {
			if _, e := tx.Exec(cleanup, "DELETE FROM app.economy_policies WHERE id=$1", policyIDs[i]); e != nil {
				t.Error("tracked policy cleanup")
				return
			}
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.wallet_accounts WHERE owner_id=ANY($1::text[])", accountIDs); e != nil {
			t.Error("tracked wallet cleanup")
			return
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.access_roles WHERE id=ANY($1::text[])", roleIDs); e != nil {
			t.Error("tracked role cleanup")
			return
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])", accountIDs); e != nil {
			t.Error("tracked identity cleanup")
			return
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.identity_challenges WHERE installation_id=ANY($1::text[])", installationIDs); e != nil {
			t.Error("tracked challenge cleanup")
			return
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.identity_installations WHERE id=ANY($1::text[])", installationIDs); e != nil {
			t.Error("tracked installation cleanup")
			return
		}
		hashes := []string{}
		for _, id := range accountIDs {
			hashes = append(hashes, identity.Digest("profiles-access:"+id))
		}
		if _, e := tx.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=ANY($1::text[])", hashes); e != nil {
			t.Error("tracked rate cleanup")
			return
		}
		if e := tx.Commit(cleanup); e != nil {
			t.Error("financial cleanup commit")
		}
	}()
	signup := func() (string, access.Credentials) {
		t.Helper()
		install := access.ID()
		installationIDs = append(installationIDs, install)
		challenge, e := auth.Challenge(ctx, install, "login")
		if e != nil {
			t.Fatal(e)
		}
		out, e := auth.Google(ctx, install, challenge.Token, nonce+install)
		if e != nil {
			t.Fatal(e)
		}
		id := out.Tokens.AccountID
		accountIDs = append(accountIDs, id)
		return id, access.Credentials{Access: out.Tokens.Access, Installation: install}
	}
	owner, oc := signup()
	sender, sc := signup()
	recipient, rc := signup()
	host, hc := signup()
	agencyID, _ := signup()
	outsider, xc := signup()
	_ = outsider
	var initialized int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM app.wallet_accounts WHERE owner_id=ANY($1::text[])", accountIDs).Scan(&initialized); e != nil || initialized != 2*len(accountIDs) {
		t.Fatal("signup did not atomically initialize available and held accounts")
	}
	transactions, e := platform.NewTransactions(operator, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if e := transactions.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		return accesspg.Bootstrap(ctx, tx, owner, "isolated financial fixture", "financial-bootstrap")
	}); e != nil {
		t.Fatal(e)
	}
	sequence := 0
	command := func(kind string) wallet.Command {
		sequence++
		return wallet.Command{Kind: kind, Key: fmt.Sprintf("fixture-%s-%d", nonce, sequence), Correlation: "financial-fixture", Reason: "synthetic financial verification", Reference: "fixture-source"}
	}
	execute := func(service *wallet.Service, c access.Credentials, in wallet.Command) any {
		t.Helper()
		out, e := service.Execute(ctx, c, in)
		if e != nil {
			t.Fatalf("financial %s failed: %v", in.Kind, e)
		}
		return out
	}
	expect := func(e error, code apperror.Code) {
		t.Helper()
		if e == nil || apperror.Public(e).Code() != code {
			t.Fatalf("wanted %s got %v", code, e)
		}
	}
	read := func(service *wallet.Service, c access.Credentials, q wallet.Query) wallet.ReadResult {
		t.Helper()
		out, e := service.Read(ctx, c, q)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	getBalance := func(service *wallet.Service, c access.Credentials) wallet.Balance {
		return decodeFinance[wallet.Balance](t, read(service, c, wallet.Query{Kind: "balance"}).Data)
	}
	latest := map[string]string{}
	effective := time.Now().UTC().Add(-time.Hour)
	savePolicy := func(p wallet.Policy) wallet.Policy {
		t.Helper()
		index := p.Kind + ":" + p.Scope
		p.PreviousID = latest[index]
		effective = effective.Add(time.Second)
		p.EffectiveAt = effective
		in := command("policy")
		in.Policy = &p
		stored := decodeFinance[wallet.Policy](t, execute(engine, oc, in))
		policyIDs = append(policyIDs, stored.ID)
		latest[index] = stored.ID
		return stored
	}
	diagnosticCreditPending := true
	credit := func(id string, amount int64) {
		t.Helper()
		if diagnosticCreditPending {
			diagnosticCreditPending = false
			originalGuard := repo.guard
			repo.guard = diagnosticFinancialGuard{delegate: originalGuard, t: t}
			defer func() { repo.guard = originalGuard }()
		}
		in := command("adjust")
		in.Target = id
		in.Amount = amount
		in.Direction = "credit"
		execute(engine, oc, in)
	}
	transfer := func(target string, amount int64) wallet.Command {
		in := command("transfer")
		in.Target = target
		in.Amount = amount
		return in
	}
	t.Run("initialization_configuration_and_issuance", func(t *testing.T) {
		// Read-only metadata can distinguish an invoker-rights deferred audit
		// trigger from a callback failure without querying financial/private data.
		for _, name := range []string{"wallet_post", "wallet_operation_balanced"} {
			rows, e := operator.Query(ctx, "SELECT p.prosecdef FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='app' AND p.proname=$1", name)
			if e != nil {
				t.Fatal("safe function metadata unavailable")
			}
			for rows.Next() {
				var definer bool
				if e := rows.Scan(&definer); e != nil {
					t.Fatal("safe function metadata scan")
				}
				t.Logf("function metadata: %s security_definer=%t", name, definer)
			}
			if e := rows.Err(); e != nil {
				t.Fatal("safe function metadata read")
			}
			rows.Close()
		}
		var auditSelect, auditInsert bool
		if e := operator.QueryRow(ctx, "SELECT has_table_privilege($1,'app.access_audit','SELECT'),has_table_privilege($1,'app.access_audit','INSERT')", app.User).Scan(&auditSelect, &auditInsert); e != nil {
			t.Fatal("safe audit-grant metadata unavailable")
		}
		t.Logf("runtime audit grants: select=%t insert=%t", auditSelect, auditInsert)

		if b := getBalance(engine, sc); b.Available != 0 || b.Held != 0 || b.AccountID != sender || b.Unit != "diamonds" {
			t.Fatal("wallet initialization")
		}
		_, e := engine.Execute(ctx, sc, transfer(recipient, 1))
		expect(e, wallet.ConfigurationRequired)
		in := command("adjust")
		in.Target = sender
		in.Amount = 1
		in.Direction = "credit"
		_, e = engine.Execute(ctx, sc, in)
		expect(e, apperror.Forbidden)
		credit(sender, 100000)
		credit(recipient, 100)
		credit(host, 1000)
		if b := getBalance(second, sc); b.Available != 100000 {
			t.Fatal("issuance credit not visible across pools")
		}
	})
	var initialPolicy wallet.Policy
	t.Run("transfers_idempotency_ownership_and_policy_snapshots", func(t *testing.T) {
		initialPolicy = savePolicy(wallet.Policy{Kind: "transfer", Scope: "all", Enabled: true, Min: 1, Max: 100000, Fee: wallet.Fee{Rounding: "ceil"}})
		beforeS, beforeR := getBalance(engine, sc), getBalance(engine, rc)
		in := transfer(recipient, 12)
		first := decodeFinance[wallet.Detail](t, execute(engine, sc, in))
		again := decodeFinance[wallet.Detail](t, execute(second, sc, in))
		if !reflect.DeepEqual(first, again) {
			t.Fatal("idempotent transfer outcome changed")
		}
		if b := getBalance(engine, sc); b.Available != beforeS.Available-12 {
			t.Fatal("duplicate sender debit")
		}
		if b := getBalance(engine, rc); b.Available != beforeR.Available+12 {
			t.Fatal("duplicate recipient credit")
		}
		altered := in
		altered.Amount++
		_, e := second.Execute(ctx, sc, altered)
		expect(e, apperror.Conflict)
		_, e = engine.Execute(ctx, sc, transfer(sender, 1))
		if e == nil {
			t.Fatal("self transfer allowed")
		}
		_, e = engine.Read(ctx, xc, wallet.Query{Kind: "operation", ID: first.Operation.ID})
		expect(e, apperror.NotFound)
		details := decodeFinance[wallet.Detail](t, read(engine, oc, wallet.Query{Kind: "inspect", ID: first.Operation.ID}).Data)
		sum := new(big.Int)
		for _, entry := range details.Postings {
			sum.Add(sum, big.NewInt(entry.Delta))
		}
		if sum.Sign() != 0 || len(details.Postings) < 2 {
			t.Fatal("unbalanced operation detail")
		}
		if details.Operation.PolicyID != initialPolicy.ID || len(details.Policy) == 0 {
			t.Fatal("missing immutable policy snapshot")
		}
		next := initialPolicy
		next.Fee = wallet.Fee{Fixed: 2, BPS: 100, Rounding: "ceil"}
		savePolicy(next)
		historicalReplay := decodeFinance[wallet.Detail](t, execute(second, sc, in))
		if historicalReplay.Operation.ID != first.Operation.ID || historicalReplay.Operation.PolicyID != initialPolicy.ID || historicalReplay.Operation.Fee != 0 {
			t.Fatal("policy update changed idempotent historical outcome")
		}

		replay := decodeFinance[wallet.Detail](t, execute(engine, sc, in))
		if replay.Operation.ID != first.Operation.ID || replay.Operation.Fee != 0 {
			t.Fatal("policy change rewrote completed transfer")
		}
		feeBefore := getBalance(engine, sc)
		withFee := decodeFinance[wallet.Detail](t, execute(engine, sc, transfer(recipient, 1)))
		if withFee.Operation.Fee != 3 || getBalance(engine, sc).Available != feeBefore.Available-4 {
			t.Fatal("ceil transfer fee or debit")
		}
		disabled := next
		disabled.Enabled = false
		savePolicy(disabled)
		_, e = engine.Execute(ctx, sc, transfer(recipient, 1))
		expect(e, wallet.ConfigurationRequired)
		limited := next
		limited.Min = 5
		limited.Max = 10
		limited.Fee = wallet.Fee{Rounding: "ceil"}
		savePolicy(limited)
		for _, n := range []int64{4, 11} {
			_, e := engine.Execute(ctx, sc, transfer(recipient, n))
			if e == nil {
				t.Fatal("transfer min/max bypass")
			}
		}
		limits := limited
		limits.Min = 1
		limits.DailyLimit = 1
		limits.MonthlyLimit = 1
		savePolicy(limits)
		_, e = engine.Execute(ctx, sc, transfer(recipient, 1))
		if e == nil {
			t.Fatal("configured period limit ignored")
		}
		initialPolicy = savePolicy(wallet.Policy{Kind: "transfer", Scope: "all", Enabled: true, Min: 1, Max: 100000, Fee: wallet.Fee{Rounding: "ceil"}})
	})

	t.Run("future_policy_selection_and_revision_cas", func(t *testing.T) {
		future := initialPolicy
		future.PreviousID = latest["transfer:all"]
		future.EffectiveAt = time.Now().UTC().Add(24 * time.Hour)
		future.Enabled = false
		in := command("policy")
		in.Policy = &future
		storedFuture := decodeFinance[wallet.Policy](t, execute(engine, oc, in))
		policyIDs = append(policyIDs, storedFuture.ID)
		latest["transfer:all"] = storedFuture.ID
		stillActive := decodeFinance[wallet.Detail](t, execute(engine, sc, transfer(recipient, 1)))
		if stillActive.Operation.PolicyID != initialPolicy.ID {
			t.Fatal("future disabled policy applied before effective timestamp")
		}
		immediate := initialPolicy
		immediate.PreviousID = storedFuture.ID
		immediate.EffectiveAt = initialPolicy.EffectiveAt.Add(time.Second)
		immediate.Fee.Fixed = 1
		replacement := command("policy")
		replacement.Policy = &immediate
		stored := decodeFinance[wallet.Policy](t, execute(engine, oc, replacement))
		policyIDs = append(policyIDs, stored.ID)
		latest["transfer:all"] = stored.ID
		effectiveNow := decodeFinance[wallet.Detail](t, execute(engine, sc, transfer(recipient, 1)))
		if effectiveNow.Operation.PolicyID != stored.ID || effectiveNow.Operation.Fee != 1 {
			t.Fatal("future publication prevented an immediate correction")
		}
		stale := command("policy")
		stalePolicy := immediate
		stalePolicy.PreviousID = initialPolicy.ID
		stalePolicy.EffectiveAt = stalePolicy.EffectiveAt.Add(time.Second)
		stale.Policy = &stalePolicy
		_, e := engine.Execute(ctx, oc, stale)
		expect(e, apperror.Conflict)
		effective = effective.Add(time.Second)
		initialPolicy = savePolicy(wallet.Policy{Kind: "transfer", Scope: "all", Enabled: true, Min: 1, Max: 100000, Fee: wallet.Fee{Rounding: "ceil"}})
	})
	t.Run("ambiguous_commit_returns_prior_result_without_retry", func(t *testing.T) {
		before := getBalance(engine, sc)
		in := transfer(recipient, 3)
		original := repo.guard
		injected := &ambiguousCommitGuard{delegate: original}
		repo.guard = injected
		_, e := engine.Execute(ctx, sc, in)
		repo.guard = original
		expect(e, apperror.ServiceUnavailable)
		if injected.calls != 1 {
			t.Fatal("ambiguous commit silently retried")
		}
		if getBalance(engine, sc).Available != before.Available-3 {
			t.Fatal("ambiguous committed fixture did not commit once")
		}
		first := decodeFinance[wallet.Detail](t, execute(second, sc, in))
		again := decodeFinance[wallet.Detail](t, execute(engine, sc, in))
		if first.Operation.ID != again.Operation.ID || getBalance(engine, sc).Available != before.Available-3 {
			t.Fatal("ambiguous retry duplicated effect")
		}
	})
	t.Run("concurrent_transfers_and_duplicate_requests", func(t *testing.T) {
		before := getBalance(engine, sc)
		in := transfer(recipient, 1)
		var wg sync.WaitGroup
		outcomes := make(chan wallet.Detail, 8)
		failures := make(chan error, 8)
		for i := range 8 {
			service := engine
			if i%2 == 1 {
				service = second
			}
			wg.Go(func() {
				out, e := service.Execute(ctx, sc, in)
				if e != nil {
					failures <- e
					return
				}
				raw, _ := json.Marshal(out)
				var detail wallet.Detail
				if e := json.Unmarshal(raw, &detail); e != nil {
					failures <- e
					return
				}
				outcomes <- detail
			})
		}
		wg.Wait()
		close(failures)
		for e := range failures {
			t.Fatal(e)
		}
		close(outcomes)
		id := ""
		for v := range outcomes {
			if id == "" {
				id = v.Operation.ID
			}
			if v.Operation.ID != id {
				t.Fatal("duplicate race created operations")
			}
		}
		if getBalance(engine, sc).Available != before.Available-1 {
			t.Fatal("duplicate race spent multiple times")
		}
		poor := getBalance(engine, rc).Available
		spend := poor/2 + 1
		results := make(chan error, 2)
		one, two := transfer(sender, spend), transfer(sender, spend)
		wg.Go(func() { _, e := engine.Execute(ctx, rc, one); results <- e })
		wg.Go(func() { _, e := second.Execute(ctx, rc, two); results <- e })
		wg.Wait()
		success := 0
		for range 2 {
			if e := <-results; e == nil {
				success++
			} else if apperror.Public(e).Code() != wallet.InsufficientFunds {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("concurrent debit overspent or rejected both")
		}
	})
	t.Run("ledger_write_privileges_rollback_cancellation_and_reconciliation", func(t *testing.T) {
		for _, sql := range []string{"UPDATE app.wallet_accounts SET balance=balance+1 WHERE owner_id=$1", "UPDATE app.wallet_entries SET delta=delta+1 WHERE operation_id IN (SELECT id FROM app.wallet_operations WHERE actor_id=$1)", "DELETE FROM app.wallet_operations WHERE actor_id=$1"} {
			if _, e := pool.Exec(ctx, sql, sender); e == nil {
				t.Fatal("runtime bypassed ledger write owner/immutability")
			}
		}
		before := getBalance(engine, sc)
		cancelled, stop := context.WithCancel(ctx)
		stop()
		_, e := engine.Execute(cancelled, sc, transfer(recipient, 1))
		if e == nil || getBalance(engine, sc) != before {
			t.Fatal("canceled financial operation committed")
		}
		injected := errors.New("synthetic rollback")
		posted := false
		key := command("rollback").Key
		e = repo.guard.Financial(ctx, oc, wallet.AdjustPermission, true, sender, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
			q := walletsql.New(tx)
			op := walletsql.InsertOperationParams{ID: access.ID(), Kind: "adjustment", ActorID: owner, TargetID: sender, IdempotencyKey: key, Fingerprint: identity.Digest(key), Amount: 1, PolicySnapshot: `{}`, Reference: "rollback-fixture", CreatedAt: timestamp(time.Now())}
			if e := post(ctx, q, op, map[string]int64{"platform:issuance": -1, "user:" + sender: 1}); e != nil {
				return e
			}
			posted = true
			return injected
		})
		// The authorization boundary deliberately maps internal callback errors to
		// a safe public error; prove the fault happened after posting, then prove
		// both the balance and immutable operation were rolled back.
		var remaining int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM app.wallet_operations WHERE actor_id=$1 AND idempotency_key=$2", owner, key).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if !posted || apperror.Public(e).Code() != apperror.ServiceUnavailable || remaining != 0 || getBalance(engine, sc) != before {
			t.Fatal("post/write error failed to roll back atomically")
		}

		afterPost, cancelAfterPost := context.WithCancel(ctx)
		lateKey := command("cancel-after-post").Key
		lateErr := repo.guard.Financial(afterPost, oc, wallet.AdjustPermission, true, sender, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
			q := walletsql.New(tx)
			op := walletsql.InsertOperationParams{ID: access.ID(), Kind: "adjustment", ActorID: owner, TargetID: sender, IdempotencyKey: lateKey, Fingerprint: identity.Digest(lateKey), Amount: 1, PolicySnapshot: `{}`, Reference: "cancel-after-post", CreatedAt: timestamp(time.Now())}
			if e := post(ctx, q, op, map[string]int64{"platform:issuance": -1, "user:" + sender: 1}); e != nil {
				return e
			}
			cancelAfterPost()
			return nil
		})
		cancelAfterPost()
		if lateErr == nil || getBalance(engine, sc) != before {
			t.Fatal("late cancellation committed financial postings")
		}
		original := repo.guard
		repo.guard = unavailableAuditGuard{delegate: original}
		auditCommand := command("adjust")
		auditCommand.Target = sender
		auditCommand.Amount = 2
		auditCommand.Direction = "credit"
		_, auditErr := engine.Execute(ctx, oc, auditCommand)
		repo.guard = original
		expect(auditErr, apperror.ServiceUnavailable)
		if getBalance(engine, sc) != before {
			t.Fatal("failed audit committed a ledger mutation")
		}
		mismatches, e := walletsql.New(pool).Reconcile(ctx)
		if e != nil || len(mismatches) != 0 {
			t.Fatal("unexplained ledger reconciliation difference")
		}
		in := command("adjust")
		in.Target = sender
		in.Amount = math.MaxInt64
		in.Direction = "credit"
		_, e = engine.Execute(ctx, oc, in)
		if e == nil || getBalance(engine, sc) != before {
			t.Fatal("balance overflow committed")
		}
	})
	t.Run("gift_conservation_scope_snapshots_and_duplicate_sends", func(t *testing.T) {
		gift := command("gift")
		gift.Target = host
		gift.Quantity = 1
		gift.GiftID = "fixture-rose"
		_, e := engine.Execute(ctx, sc, gift)
		expect(e, wallet.ConfigurationRequired)
		savePolicy(wallet.Policy{Kind: "gift", Scope: "non_agency", Enabled: true, VAT: "inclusive_diamonds", TaxJurisdiction: "synthetic-jurisdiction", Rounding: "largest_remainder", HostBPS: 7000, PlatformBPS: 2500, TaxBPS: 500, Catalog: map[string]int64{"fixture-rose": 101}})
		before := getBalance(engine, hc)
		first := decodeFinance[wallet.Detail](t, execute(engine, sc, gift))
		duplicate := decodeFinance[wallet.Detail](t, execute(second, sc, gift))
		if first.Operation.ID != duplicate.Operation.ID || getBalance(engine, hc).Available != before.Available+71 {
			t.Fatal("non-agency allocation/idempotency")
		}
		association := command("association")
		association.Association = &wallet.Association{AccountID: host, AgencyID: agencyID, Classification: agency.AudioHost, EffectiveFrom: time.Now().Add(-2 * time.Hour)}
		execute(engine, oc, association)
		agencyGift := command("gift")
		agencyGift.Target = host
		agencyGift.Quantity = 1
		agencyGift.GiftID = "fixture-rose"
		_, e = engine.Execute(ctx, sc, agencyGift)
		expect(e, wallet.ConfigurationRequired)
		p := savePolicy(wallet.Policy{Kind: "gift", Scope: "agency", Enabled: true, VAT: "inclusive_diamonds", TaxJurisdiction: "synthetic-jurisdiction", Rounding: "largest_remainder", HostBPS: 5000, AgencyBPS: 2000, PlatformBPS: 2500, TaxBPS: 500, Catalog: map[string]int64{"fixture-rose": 101}})
		var agencyBefore int64
		if e := pool.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id=$1", "user:"+agencyID).Scan(&agencyBefore); e != nil {
			t.Fatal(e)
		}
		result := decodeFinance[wallet.Detail](t, execute(engine, sc, agencyGift))
		if result.Operation.PolicyID != p.ID {
			t.Fatal("agency gift policy snapshot")
		}
		var agencyAfter int64
		if e := pool.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id=$1", "user:"+agencyID).Scan(&agencyAfter); e != nil || agencyAfter != agencyBefore+20 {
			t.Fatal("agency gift commission")
		}
		beforeSender, beforeHost := getBalance(engine, sc), getBalance(engine, hc)
		results := make(chan error, 2)
		one, two := agencyGift, agencyGift
		one.Key, two.Key = command("gift").Key, command("gift").Key
		var wg sync.WaitGroup
		wg.Go(func() { _, e := engine.Execute(ctx, sc, one); results <- e })
		wg.Go(func() { _, e := second.Execute(ctx, sc, two); results <- e })
		wg.Wait()
		for range 2 {
			if e := <-results; e != nil {
				t.Fatal(e)
			}
		}
		if getBalance(engine, sc).Available != beforeSender.Available-202 || getBalance(engine, hc).Available != beforeHost.Available+102 {
			t.Fatal("concurrent gifts lost a debit or distribution")
		}
	})
	var withdrawalPolicy wallet.Policy
	t.Run("withdrawal_reserve_review_release_and_no_settlement", func(t *testing.T) {
		request := command("withdrawal")
		request.Amount = 5
		_, e := engine.Execute(ctx, sc, request)
		expect(e, wallet.ConfigurationRequired)
		withdrawalPolicy = savePolicy(wallet.Policy{Kind: "withdrawal", Scope: "all", Enabled: true, ReviewOnly: true, Min: 1, Max: 100000, Fee: wallet.Fee{Fixed: 1, Rounding: "ceil"}})
		eligible := decodeFinance[wallet.Eligibility](t, read(engine, sc, wallet.Query{Kind: "eligibility"}).Data)
		if !eligible.Eligible || eligible.Settlement != notConfigured {
			t.Fatal("ordinary review-only withdrawal eligibility")
		}
		before := getBalance(engine, sc)
		w := decodeFinance[wallet.Withdrawal](t, execute(engine, sc, request))
		if w.State != "pending" || w.Settlement != notConfigured || w.Reserved != 6 {
			t.Fatal("request incorrectly settled")
		}
		if b := getBalance(engine, sc); b.Available != before.Available-6 || b.Held != before.Held+6 {
			t.Fatal("reservation accounting")
		}
		duplicate := decodeFinance[wallet.Withdrawal](t, execute(second, sc, request))
		if duplicate.ID != w.ID || getBalance(engine, sc).Held != before.Held+6 {
			t.Fatal("withdrawal duplicate reserved again")
		}
		review := command("transition")
		review.ID = w.ID
		review.Action = "approve"
		_, e = engine.Execute(ctx, sc, review)
		expect(e, apperror.Forbidden)
		approved := decodeFinance[wallet.Withdrawal](t, execute(engine, oc, review))
		if approved.State != "approved" || approved.Settlement != notConfigured || getBalance(engine, sc).Held != before.Held+6 {
			t.Fatal("approval captured/burned held funds")
		}
		execute(second, oc, review)
		cancelCommand := command("transition")
		cancelCommand.ID = w.ID
		cancelCommand.Action = "cancel"
		_, e = engine.Execute(ctx, sc, cancelCommand)
		expect(e, apperror.Conflict)
		reject := command("transition")
		reject.ID = w.ID
		reject.Action = "reject"
		rejected := decodeFinance[wallet.Withdrawal](t, execute(engine, oc, reject))
		if rejected.State != "rejected" || getBalance(engine, sc) != before {
			t.Fatal("rejection did not release exact reservation")
		}
		execute(second, oc, reject)
		if getBalance(engine, sc) != before {
			t.Fatal("repeated rejection double credited")
		}
		nextRequest := command("withdrawal")
		nextRequest.Amount = 5
		pending := decodeFinance[wallet.Withdrawal](t, execute(engine, sc, nextRequest))
		cancelCommand = command("transition")
		cancelCommand.ID = pending.ID
		cancelCommand.Action = "cancel"
		_, e = engine.Execute(ctx, xc, cancelCommand)
		expect(e, apperror.NotFound)
		cancelled := decodeFinance[wallet.Withdrawal](t, execute(engine, sc, cancelCommand))
		if cancelled.State != "cancelled" || getBalance(engine, sc) != before {
			t.Fatal("owner cancellation reservation release")
		}
		unsupported := withdrawalPolicy
		unsupported.ReviewOnly = false
		unsupported.PreviousID = latest["withdrawal:all"]
		invalidPolicy := command("policy")
		invalidPolicy.Policy = &unsupported
		_, e = engine.Execute(ctx, oc, invalidPolicy)
		expect(e, wallet.ConfigurationRequired)
		disabled := withdrawalPolicy
		disabled.Enabled = false
		savePolicy(disabled)
		blocked := command("withdrawal")
		blocked.Amount = 5
		_, e = engine.Execute(ctx, sc, blocked)
		expect(e, wallet.ConfigurationRequired)
		withdrawalPolicy = savePolicy(withdrawalPolicy)
		beforeConcurrent := getBalance(engine, sc)
		one, two := command("withdrawal"), command("withdrawal")
		one.Amount, two.Amount = 5, 5
		type outcome struct {
			value any
			err   error
		}
		results := make(chan outcome, 2)
		var wg sync.WaitGroup
		wg.Go(func() { v, e := engine.Execute(ctx, sc, one); results <- outcome{v, e} })
		wg.Go(func() { v, e := second.Execute(ctx, sc, two); results <- outcome{v, e} })
		wg.Wait()
		reservations := make([]wallet.Withdrawal, 0, 2)
		for range 2 {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			reservations = append(reservations, decodeFinance[wallet.Withdrawal](t, result.value))
		}
		if b := getBalance(engine, sc); b.Available != beforeConcurrent.Available-12 || b.Held != beforeConcurrent.Held+12 || reservations[0].ID == reservations[1].ID {
			t.Fatal("concurrent withdrawal reservations lost funds or identity")
		}
		cancelRace, rejectRace := command("transition"), command("transition")
		cancelRace.ID, rejectRace.ID = reservations[0].ID, reservations[0].ID
		cancelRace.Action, rejectRace.Action = "cancel", "reject"
		wg.Go(func() { v, e := engine.Execute(ctx, sc, cancelRace); results <- outcome{v, e} })
		wg.Go(func() { v, e := second.Execute(ctx, oc, rejectRace); results <- outcome{v, e} })
		wg.Wait()
		succeeded := 0
		for range 2 {
			if result := <-results; result.err == nil {
				succeeded++
			} else {
				expect(result.err, apperror.Conflict)
			}
		}
		if b := getBalance(engine, sc); succeeded != 1 || b.Available != beforeConcurrent.Available-6 || b.Held != beforeConcurrent.Held+6 {
			t.Fatal("concurrent terminal transitions released a reservation twice")
		}
		cancelRemaining := command("transition")
		cancelRemaining.ID, cancelRemaining.Action = reservations[1].ID, "cancel"
		execute(engine, sc, cancelRemaining)
		if getBalance(engine, sc) != beforeConcurrent {
			t.Fatal("concurrent withdrawal cleanup did not conserve funds")
		}
	})
	t.Run("agency_verified_activity_and_policy_binding", func(t *testing.T) {
		activity := agency.Policy{Scope: agency.AudioHost, Timezone: "UTC", Period: agency.PreviousCompletedCalendarMonth, MinimumQualifyingDays: 15, MinimumDailyLiveMinutes: 120}
		p := savePolicy(wallet.Policy{Kind: "activity", Scope: agency.AudioHost, Enabled: true, Activity: &activity})
		unavailable := decodeFinance[wallet.Eligibility](t, read(engine, hc, wallet.Query{Kind: "eligibility"}).Data)
		if unavailable.Eligible || !strings.Contains(strings.Join(unavailable.Reasons, ","), agency.ReasonUnavailable) {
			t.Fatal("unavailable verified activity opened withdrawal")
		}
		request := command("withdrawal")
		request.Amount = 5
		_, e := engine.Execute(ctx, hc, request)
		expect(e, wallet.ActivityUnavailable)
		trustedRepo, e := New(other, verifiedActivityFixture{days: 15, minutes: 120})
		if e != nil {
			t.Fatal(e)
		}
		trusted := wallet.New(trustedRepo)
		w := decodeFinance[wallet.Withdrawal](t, execute(trusted, hc, request))
		if w.Settlement != notConfigured {
			t.Fatal("activity fixture caused settlement")
		}
		var snapshotVersion string
		if e := operator.QueryRow(ctx, "SELECT eligibility_snapshot->'activity'->>'policy_version' FROM app.wallet_withdrawals WHERE id=$1", w.ID).Scan(&snapshotVersion); e != nil || snapshotVersion != p.ID {
			t.Fatal("activity policy snapshot missing")
		}
		changed := activity
		changed.MinimumQualifyingDays = 16
		newPolicy := savePolicy(wallet.Policy{Kind: "activity", Scope: agency.AudioHost, Enabled: true, Activity: &changed})
		_ = newPolicy
		nextRequest := command("withdrawal")
		nextRequest.Amount = 5
		_, e = trusted.Execute(ctx, hc, nextRequest)
		expect(e, wallet.NotEligible)
		var historic string
		if e := operator.QueryRow(ctx, "SELECT eligibility_snapshot->'activity'->>'policy_version' FROM app.wallet_withdrawals WHERE id=$1", w.ID).Scan(&historic); e != nil || historic != p.ID {
			t.Fatal("policy change rewrote historical eligibility")
		}

		savePolicy(wallet.Policy{Kind: "activity", Scope: agency.AudioHost, Enabled: true, Activity: &activity})
		fourteenRepo, e := New(other, verifiedActivityFixture{days: 14, minutes: 480})
		if e != nil {
			t.Fatal(e)
		}
		v := decodeFinance[wallet.Eligibility](t, read(wallet.New(fourteenRepo), hc, wallet.Query{Kind: "eligibility"}).Data)
		if v.Eligible {
			t.Fatal("extra hours replaced a missing qualifying day")
		}
		shortDayRepo, e := New(other, verifiedActivityFixture{days: 15, minutes: 119})
		if e != nil {
			t.Fatal(e)
		}
		shortDays := decodeFinance[wallet.Eligibility](t, read(wallet.New(shortDayRepo), hc, wallet.Query{Kind: "eligibility"}).Data)
		if shortDays.Eligible {
			t.Fatal("119 minutes rounded to a qualifying day")
		}
		beforeRace := getBalance(engine, hc)
		collectionCalls := 0
		changingRepo, e := New(other, verifiedActivityFixture{days: 15, minutes: 120, before: func() {
			collectionCalls++
			savePolicy(wallet.Policy{Kind: "activity", Scope: agency.AudioHost, Enabled: true, Activity: &activity})
		}})
		if e != nil {
			t.Fatal(e)
		}
		raced := command("withdrawal")
		raced.Amount = 5
		_, e = wallet.New(changingRepo).Execute(ctx, hc, raced)
		expect(e, wallet.ActivityUnavailable)
		if collectionCalls != 1 || getBalance(engine, hc) != beforeRace {
			t.Fatal("changed activity policy authorized a stale snapshot or retried collection")
		}
		videoPolicy := activity
		videoPolicy.Scope = agency.VideoHost
		savePolicy(wallet.Policy{Kind: "activity", Scope: agency.VideoHost, Enabled: true, Activity: &videoPolicy})
		classification := command("association")
		classification.Association = &wallet.Association{AccountID: host, AgencyID: agencyID, Classification: agency.VideoHost, EffectiveFrom: time.Now().Add(-time.Hour)}
		execute(engine, oc, classification)
		video := decodeFinance[wallet.Eligibility](t, read(trusted, hc, wallet.Query{Kind: "eligibility"}).Data)
		if !video.Eligible || video.Classification != agency.VideoHost || video.Activity == nil || video.Activity.QualifyingDays != 15 {
			t.Fatal("video-host activity policy scope not selected")
		}
	})
	t.Run("financial_permissions_revocation_and_final_reconciliation", func(t *testing.T) {
		accessRepo, e := accesspg.New(pool)
		if e != nil {
			t.Fatal(e)
		}
		staffService, e := access.New(accessRepo)
		if e != nil {
			t.Fatal(e)
		}
		r, e := staffService.PutRole(ctx, oc, "", "financial-observer-"+nonce, []string{wallet.OperationsRead}, true, "synthetic narrow permission", "finance-role")
		if e != nil {
			t.Fatal(e)
		}
		roleIDs = append(roleIDs, r.ID)
		if e := staffService.Assign(ctx, oc, recipient, []string{r.ID}, "synthetic assignment", "finance-assign"); e != nil {
			t.Fatal(e)
		}
		history := decodeFinance[[]wallet.Operation](t, read(engine, sc, wallet.Query{Kind: "history", Limit: 1}).Data)
		if len(history) != 1 {
			t.Fatal("missing financial history")
		}
		if _, e := second.Read(ctx, rc, wallet.Query{Kind: "inspect", ID: history[0].ID}); e != nil {
			t.Fatal("explicit financial inspection permission denied")
		}
		if e := staffService.Assign(ctx, oc, recipient, []string{}, "synthetic revocation", "finance-revoke"); e != nil {
			t.Fatal(e)
		}
		_, e = second.Read(ctx, rc, wallet.Query{Kind: "inspect", ID: history[0].ID})
		expect(e, apperror.Forbidden)
		adjustments := command("adjust")
		adjustments.Target = recipient
		adjustments.Amount = 1
		adjustments.Direction = "credit"
		_, e = second.Execute(ctx, rc, adjustments)
		expect(e, apperror.Forbidden)
		differences, e := walletsql.New(pool).Reconcile(ctx)
		if e != nil || len(differences) != 0 {
			t.Fatal("final financial reconciliation differs")
		}
		var missing int
		if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.wallet_operations o WHERE (o.actor_id=ANY($1::text[]) OR o.target_id=ANY($1::text[])) AND (SELECT COALESCE(sum(e.delta::numeric),1) FROM app.wallet_entries e WHERE e.operation_id=o.id)<>0", accountIDs).Scan(&missing); e != nil || missing != 0 {
			t.Fatal("unbalanced financial operation persisted")
		}
		other.Close()
		_, e = second.Read(ctx, rc, wallet.Query{Kind: "balance"})
		expect(e, apperror.ServiceUnavailable)
	})
	if !t.Failed() {
		t.Log("PASS: guarded isolated atomic signup/wallets, issuance, transfers/concurrency/idempotency, policy gates/snapshots, gifts/conservation, holds/review/release, authoritative activity boundary, financial permissions/revocation and reconciliation; no provider settlement")
	}
}
