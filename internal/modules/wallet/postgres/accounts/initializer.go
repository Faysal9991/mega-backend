// Package accounts provides only Wallet-owned transaction-scoped zero-account
// initialization, allowing Identity signup without a repository import cycle.
package accounts

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/jackc/pgx/v5/pgtype"
)

type Initializer struct{ q *walletsql.Queries }

func Bind(db walletsql.DBTX) *Initializer { return &Initializer{walletsql.New(db)} }
func (i *Initializer) Initialize(ctx context.Context, id string) error {
	return i.q.Initialize(ctx, pgtype.Text{String: id, Valid: true})
}
