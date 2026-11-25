// Package bindings owns push unbinding inside Identity's existing transaction.
package bindings

import (
	"context"

	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
)

func Installation(ctx context.Context, db deliverysql.DBTX, id string) error {
	return deliverysql.New(db).UnbindInstallation(ctx, id)
}
func Session(ctx context.Context, db deliverysql.DBTX, id string) error {
	return deliverysql.New(db).UnbindSession(ctx, id)
}
func Account(ctx context.Context, db deliverysql.DBTX, id string) error {
	return deliverysql.New(db).UnbindAccount(ctx, id)
}

func Prune(ctx context.Context, db deliverysql.DBTX, account string) error {
	return deliverysql.New(db).PruneRevokedBindings(ctx, account)
}

// New-session account proof authorizes only its own prior binding. A caller's
// resettable installation ID cannot clear another account's active token.
func OwnInstallation(ctx context.Context, db deliverysql.DBTX, installation, account string) error {
	return deliverysql.New(db).RemoveDevice(ctx, deliverysql.RemoveDeviceParams{InstallationID: installation, AccountID: account})
}
