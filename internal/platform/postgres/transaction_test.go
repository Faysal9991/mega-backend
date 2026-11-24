package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeBeginner struct {
	tx      pgx.Tx
	err     error
	calls   int
	ctx     context.Context
	options pgx.TxOptions
}

func (b *fakeBeginner) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	b.calls++
	b.ctx = ctx
	b.options = options
	return b.tx, b.err
}

type fakeTransaction struct {
	pgx.Tx             // Unused query methods deliberately fail if unexpectedly invoked.
	commits, rollbacks int
	commit             func(context.Context) error
	rollback           func(context.Context) error
}

func (tx *fakeTransaction) Commit(ctx context.Context) error {
	tx.commits++
	if tx.commit != nil {
		return tx.commit(ctx)
	}
	return nil
}
func (tx *fakeTransaction) Rollback(ctx context.Context) error {
	tx.rollbacks++
	if tx.rollback != nil {
		return tx.rollback(ctx)
	}
	return nil
}

type txContextKey struct{}

func TestTransactionOutcomes(t *testing.T) {
	callbackErr := errors.New("PRIVATE_SENTINEL_CALLBACK")
	commitErr := &pgconn.PgError{Code: "08006", Message: "PRIVATE_SENTINEL_AMBIGUOUS"}
	cleanupErr := errors.New("PRIVATE_SENTINEL_CLEANUP")
	for _, mode := range []string{"success", "callback", "begin", "cancel", "commit", "cleanup", "closed rollback"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), txContextKey{}, "trace"))
			defer cancel()
			tx := &fakeTransaction{}
			begin := &fakeBeginner{tx: tx}
			if mode == "begin" {
				begin.err = callbackErr
			}
			if mode == "commit" {
				tx.commit = func(got context.Context) error {
					if got != ctx {
						t.Fatal("commit lost context")
					}
					return commitErr
				}
			}
			tx.rollback = func(cleanup context.Context) error {
				if cleanup.Err() != nil || cleanup.Value(txContextKey{}) != "trace" {
					t.Fatal("cleanup inherited cancellation or lost values")
				}
				deadline, ok := cleanup.Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Fatal("cleanup is unbounded")
				}
				if mode == "cleanup" {
					return cleanupErr
				}
				if mode == "closed rollback" {
					return pgx.ErrTxClosed
				}
				return nil
			}
			helper, err := NewTransactions(begin, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			options := pgx.TxOptions{IsoLevel: pgx.Serializable}
			calls := 0
			err = helper.Within(ctx, options, func(got context.Context, actual pgx.Tx) error {
				calls++
				if got != ctx || actual != tx {
					t.Fatal("callback lost context or transaction")
				}
				switch mode {
				case "callback", "cleanup", "closed rollback":
					return callbackErr
				case "cancel":
					cancel()
				}
				return nil
			})
			if begin.calls != 1 || begin.ctx != ctx || begin.options != options {
				t.Fatal("begin retried or lost context/options")
			}
			switch mode {
			case "success":
				if err != nil || tx.commits != 1 || tx.rollbacks != 0 {
					t.Fatal("successful transaction outcome")
				}
			case "begin":
				if !errors.Is(err, callbackErr) || calls != 0 || tx.commits != 0 || tx.rollbacks != 0 {
					t.Fatal("begin failure outcome")
				}
			case "commit":
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr != commitErr || !errors.Is(err, commitErr) || tx.commits != 1 || tx.rollbacks != 1 || calls != 1 {
					t.Fatal("ambiguous commit classification or retry")
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || tx.commits != 0 || tx.rollbacks != 1 {
					t.Fatal("cancellation outcome")
				}
			default:
				if !errors.Is(err, callbackErr) || tx.commits != 0 || tx.rollbacks != 1 {
					t.Fatal("callback failure outcome")
				}
			}
			if mode == "cleanup" && !errors.Is(err, cleanupErr) {
				t.Fatal("rollback classification lost")
			}
			if err != nil && strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "PRIVATE_SENTINEL") {
				t.Fatal("transaction error exposed private cause")
			}
		})
	}
}
func TestTransactionPanicAndBoundedCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := &struct{ name string }{"PRIVATE_SENTINEL"}
	cleanupDone := false
	tx := &fakeTransaction{rollback: func(cleanup context.Context) error {
		if cleanup.Err() != nil {
			t.Error("cleanup canceled before rollback")
		}
		<-cleanup.Done()
		cleanupDone = true
		return cleanup.Err()
	}}
	helper, err := NewTransactions(&fakeBeginner{tx: tx}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() != sentinel || !cleanupDone {
				t.Error("panic changed or propagated before cleanup")
			}
		}()
		_ = helper.Within(ctx, pgx.TxOptions{}, func(context.Context, pgx.Tx) error { cancel(); panic(sentinel) })
	}()
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatal("panic committed or leaked transaction")
	}
}
func TestTransactionTimeoutAndConstructor(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, 6 * time.Second} {
		if _, err := NewTransactions(&fakeBeginner{}, timeout); err == nil {
			t.Fatal("unbounded cleanup accepted")
		}
	}
	if _, err := NewTransactions(nil, time.Second); err == nil {
		t.Fatal("nil beginner")
	}
	tx := &fakeTransaction{rollback: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	begin := &fakeBeginner{tx: tx}
	helper, _ := NewTransactions(begin, time.Millisecond)
	sentinel := errors.New("callback failed")
	err := helper.Within(context.Background(), pgx.TxOptions{}, func(context.Context, pgx.Tx) error { return sentinel })
	if !errors.Is(err, sentinel) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup timeout masked callback or lost classification")
	}
	if helper.Within(context.Background(), pgx.TxOptions{}, nil) == nil || begin.calls != 1 {
		t.Fatal("nil callback began a transaction")
	}
}
