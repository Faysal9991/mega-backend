package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestFailureDiagnosticsKeepCausePrivate(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, &pgconn.PgError{Code: "55P03", Message: "PRIVATE_SENTINEL"}, errors.New("PRIVATE_SENTINEL")} {
		e := safeFailure("migration upgrade", cause)
		if strings.Contains(fmt.Sprintf("%s %#v", e, e), "PRIVATE_SENTINEL") || !errors.Is(e, cause) {
			t.Fatal("unsafe or lost cause")
		}
	}
}
