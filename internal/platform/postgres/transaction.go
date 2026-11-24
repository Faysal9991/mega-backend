package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TransactionBeginner is satisfied directly by pgxpool.Pool. Callbacks must not
// commit/rollback themselves, retain tx, or do external I/O inside a transaction.
type TransactionBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Transactions struct {
	beginner       TransactionBeginner
	cleanupTimeout time.Duration
}

func NewTransactions(beginner TransactionBeginner, cleanupTimeout time.Duration) (*Transactions, error) {
	if beginner == nil || cleanupTimeout < time.Millisecond || cleanupTimeout > 5*time.Second {
		return nil, errors.New("postgres: transaction beginner and bounded cleanup timeout required")
	}
	return &Transactions{beginner: beginner, cleanupTimeout: cleanupTimeout}, nil
}

// transactionFailure retains private classification without formatting causes.
// Consumers may use errors.Is/As; never log/serialize the unwrapped cause.
type transactionFailure struct {
	operation string
	cause     error
}

func (e *transactionFailure) Error() string {
	return "postgres: transaction " + e.operation + " failed"
}
func (e *transactionFailure) GoString() string { return e.Error() }
func (e *transactionFailure) Unwrap() error    { return e.cause }

// Within never retries. A commit error may have an ambiguous outcome: callers
// must reconcile using their operation's authoritative state/idempotency rules.
func (t *Transactions) Within(ctx context.Context, options pgx.TxOptions, fn func(context.Context, pgx.Tx) error) (err error) {
	if fn == nil {
		return errors.New("postgres: transaction callback required")
	}
	tx, err := t.beginner.BeginTx(ctx, options)
	if err != nil {
		return &transactionFailure{operation: "begin", cause: err}
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Preserve values, detach cancellation/deadline, and apply a separate bound.
		// This defer also runs during panic unwinding without recovering/masking it.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.cleanupTimeout)
		defer cancel()
		rollbackErr := tx.Rollback(cleanup)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = &transactionFailure{operation: "cleanup", cause: errors.Join(err, rollbackErr)}
		}
	}()
	if err = fn(ctx, tx); err != nil {
		return &transactionFailure{operation: "callback", cause: err}
	}
	if err = ctx.Err(); err != nil {
		return &transactionFailure{operation: "cancellation", cause: err}
	}
	if err = tx.Commit(ctx); err != nil {
		return &transactionFailure{operation: "commit", cause: err}
	}
	committed = true
	return nil
}
