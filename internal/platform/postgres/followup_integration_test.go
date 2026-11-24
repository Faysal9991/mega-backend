//go:build integration

package postgres

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func followupSettings(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated test acknowledgment required")
	}
	cfg, err := config.LoadDatabase(func(key string) (string, bool) { return os.LookupEnv(prefix + key) }, config.Test)
	if err != nil || cfg.Name != "biho_live_test" || cfg.User != role {
		t.Fatal("dedicated test configuration unavailable or invalid")
	}
	cfg.MaxConns = 2
	return cfg
}
func TestIntegrationTransactionsAndAvailability(t *testing.T) {
	app := followupSettings(t, "TEST_", "biho_live_test_app")
	migration := followupSettings(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	var database, role string
	if err := migrator.db.QueryRowContext(ctx, "SELECT current_database(),current_user").Scan(&database, &role); err != nil || database != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("authenticated test identity mismatch")
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.db.ExecContext(ctx, "CREATE TABLE app.step6_followup_fixture(id bigint PRIMARY KEY)"); err != nil {
		t.Fatal("isolated fixture creation failed")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := migrator.db.ExecContext(cleanup, "DROP TABLE app.step6_followup_fixture"); err != nil {
			t.Error("isolated fixture cleanup failed")
		}
	}()
	if _, err := migrator.db.ExecContext(ctx, "GRANT SELECT,INSERT ON app.step6_followup_fixture TO biho_live_test_app"); err != nil {
		t.Fatal("fixture grant failed")
	}
	pool, err := Open(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	helper, err := NewTransactions(pool, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id int64) func(context.Context, pgx.Tx) error {
		return func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO app.step6_followup_fixture(id) VALUES($1)", id)
			return err
		}
	}
	if err := helper.Within(ctx, pgx.TxOptions{}, insert(1)); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("synthetic callback failure")
	err = helper.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if err := insert(2)(ctx, tx); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal("callback classification lost")
	}
	panicCtx, stopPanic := context.WithCancel(ctx)
	func() {
		defer func() {
			if recover() != sentinel {
				t.Error("panic classification lost")
			}
		}()
		_ = helper.Within(panicCtx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			if err := insert(3)(ctx, tx); err != nil {
				t.Error("panic fixture insertion failed")
			}
			stopPanic()
			panic(sentinel)
		})
	}()
	stopPanic()
	cancelCtx, stopTx := context.WithCancel(ctx)
	err = helper.Within(cancelCtx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if err := insert(4)(ctx, tx); err != nil {
			return err
		}
		stopTx()
		return nil
	})
	stopTx()
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation classification lost")
	}
	err = helper.Within(ctx, pgx.TxOptions{}, insert(1))
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23505" {
		t.Fatal("SQLSTATE classification lost")
	}
	callbacks := 0
	err = helper.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		callbacks++
		if err := insert(5)(ctx, tx); err != nil {
			return err
		}
		_ = insert(1)(ctx, tx)
		return nil
	})
	if !errors.Is(err, pgx.ErrTxCommitRollback) || callbacks != 1 {
		t.Fatal("failed commit not classified or callback retried")
	}
	var count int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM app.step6_followup_fixture").Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback/panic/cancellation committed rows")
	}
	if pool.Stat().AcquiredConns() != 0 || pool.Ping(ctx) != nil {
		t.Fatal("transaction cleanup leaked or poisoned pool")
	}

	counted := &gatedPoolPinger{pool: pool, entered: make(chan struct{}), release: make(chan struct{})}
	checker, err := NewAvailability(counted, 2*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clock := &atomic.Int64{}
	clock.Store(time.Now().UnixNano())
	checker.now = func() time.Time { return time.Unix(0, clock.Load()) }
	state := httpserver.NewLifecycle(checker.Available)
	cfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logging.New(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	router, err := httpserver.NewRouter(cfg.HTTP, logger, state)
	if err != nil {
		t.Fatal(err)
	}
	server, err := httpserver.New(cfg.HTTP, logger, state, router)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &followupListener{Listener: bound, accepting: make(chan struct{}), closing: make(chan struct{})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() { _ = server.Close() }()
	select {
	case <-listener.accepting:
	case <-ctx.Done():
		t.Fatal("listener did not serve")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	probe := func(method, path string) int {
		request, err := http.NewRequestWithContext(ctx, method, "http://"+bound.Addr().String()+path, nil)
		if err != nil {
			t.Error("probe construction")
			return 0
		}
		response, err := client.Do(request)
		if err != nil {
			t.Error("probe transport failure")
			return 0
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.Header.Get("Cache-Control") != "no-store" {
			t.Error("probe response contract")
		}
		return response.StatusCode
	}
	if probe("GET", "/health/ready") != 200 {
		t.Fatal("initial database readiness")
	}
	first, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("first pool reservation")
	}
	defer first.Release()
	second, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("second pool reservation")
	}
	defer second.Release()
	clock.Add(int64(time.Second))
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if probe("GET", "/health/ready") != 503 {
				t.Error("unavailable pool reported ready")
			}
		})
	}
	wg.Wait()
	if counted.calls.Load() != 2 || !state.Ready() || probe("GET", "/health/live") != 200 {
		t.Fatal("storm duplicated checks, outage became terminal, or liveness failed")
	}
	first.Release()
	second.Release()
	clock.Add(int64(time.Second))
	if probe("HEAD", "/health/ready") != 200 {
		t.Fatal("database readiness did not recover")
	}
	if counted.calls.Load() != 3 {
		t.Fatal("recovery check count")
	}
	counted.block.Store(true)
	clock.Add(int64(time.Second))
	pending := make(chan int, 1)
	go func() { pending <- probe("GET", "/health/ready") }()
	select {
	case <-counted.entered:
	case <-ctx.Done():
		t.Fatal("recovery check not in flight")
	}
	shutdown := make(chan error, 1)
	go func() {
		shutdownCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		shutdown <- server.Shutdown(shutdownCtx)
	}()
	select {
	case <-listener.closing:
	case <-ctx.Done():
		t.Fatal("shutdown did not close listener")
	}
	close(counted.release)
	if <-pending != 503 {
		t.Fatal("in-flight recovery reported ready during shutdown")
	}
	if err := <-shutdown; err != nil {
		t.Fatal("shutdown failed")
	}
	if err := <-served; err != nil {
		t.Fatal("serve exit failed")
	}
	before := counted.calls.Load()
	for range 16 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil))
		if rec.Code != 503 {
			t.Fatal("terminal readiness restored")
		}
	}
	if counted.calls.Load() != before || state.Ready() {
		t.Fatal("stopped lifecycle checked or restored dependency")
	}
}

type gatedPoolPinger struct {
	pool             *pgxpool.Pool
	calls            atomic.Int64
	block            atomic.Bool
	entered, release chan struct{}
	once             sync.Once
}

func (p *gatedPoolPinger) Ping(ctx context.Context) error {
	p.calls.Add(1)
	if p.block.Load() {
		p.once.Do(func() { close(p.entered) })
		select {
		case <-p.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.pool.Ping(ctx)
}

type followupListener struct {
	net.Listener
	accepting, closing    chan struct{}
	acceptOnce, closeOnce sync.Once
}

func (l *followupListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.accepting) })
	return l.Listener.Accept()
}
func (l *followupListener) Close() error {
	l.closeOnce.Do(func() { close(l.closing) })
	return l.Listener.Close()
}
