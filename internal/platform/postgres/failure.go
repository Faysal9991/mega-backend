package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
)

// failure preserves inspection of the cause without printing a DSN, SQL or
// provider message. Integration diagnostics can distinguish locks and deadlines.
type failure struct {
	operation, code string
	cause           error
}

func (e *failure) Error() string { return "postgres: " + e.operation + " failed (" + e.code + ")" }
func (e *failure) Unwrap() error { return e.cause }
func safeFailure(operation string, err error) error {
	code := "DATABASE_UNAVAILABLE"
	var p *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = "DEADLINE_EXCEEDED"
	case errors.Is(err, context.Canceled):
		code = "CANCELED"
	case errors.As(err, &p):
		code = "SQLSTATE_" + p.Code
	}
	return &failure{operation, code, err}
}

func (e *failure) GoString() string { return e.Error() }
