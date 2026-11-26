package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/modules/packages/postgres/activation"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"time"
)

// PurchasePackage is a Wallet Engine operation: funds, ledger, immutable profit
// and purchased entitlement share its authoritative security transaction.
func (r *Repository) PurchasePackage(ctx context.Context, c access.Credentials, id, key string) (out packages.Purchase, err error) {
	if !access.ValidID(id) || !access.ValidCorrelation(key) {
		return out, wallet.Error(apperror.ValidationFailed)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fingerprint, e := wallet.Fingerprint(struct {
		PackageID string `json:"package_id"`
	}{id})
	if e != nil {
		return out, e
	}
	err = r.guard.Financial(ctx, c, "", false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := walletsql.New(tx)
		prior, e := q.ExistingCommand(ctx, walletsql.ExistingCommandParams{ActorID: u.Actor(), Kind: "package_purchase", IdempotencyKey: key})
		if e == nil {
			if prior.Fingerprint != fingerprint {
				return wallet.Error(apperror.Conflict)
			}
			return json.Unmarshal(prior.Result, &out)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if !u.Effective().Allows("packages.purchase") {
			return wallet.Error(apperror.Forbidden)
		}
		configured, e := q.PackageAccountingConfigured(ctx)
		if e != nil {
			return e
		}
		if !configured {
			return wallet.Error(apperror.FinancialConfigurationRequired)
		}
		now, e := q.Clock(ctx)
		if e != nil {
			return e
		}
		v, e := activation.Resolve(ctx, tx, u.Actor(), id, now.Time)
		if e != nil {
			return e
		}
		op, entitlement := access.ID(), access.ID()
		snapshot, e := json.Marshal(map[string]string{"accounting_policy": "platform_full_no_vat", "package_version_id": v.VersionID})
		if e != nil {
			return e
		}
		operation := walletsql.InsertOperationParams{ID: op, Kind: "package_purchase", ActorID: u.Actor(), TargetID: u.Actor(), IdempotencyKey: key, Fingerprint: fingerprint, Amount: v.Price, PolicySnapshot: string(snapshot), Reference: entitlement, CreatedAt: now}
		if e = q.InsertOperation(ctx, operation); e != nil {
			return e
		}
		if e = activation.Record(ctx, tx, u.Actor(), op, entitlement, v, now.Time); e != nil {
			return e
		}
		userBalance, e := q.AccountBalance(ctx, "user:"+u.Actor())
		if e != nil {
			return e
		}
		if userBalance < v.Price {
			return wallet.Error(wallet.InsufficientFunds)
		}
		platformBalance, e := q.AccountBalance(ctx, "platform:packages")
		if e != nil {
			return e
		}
		if _, e = wallet.Add(platformBalance, v.Price); e != nil {
			return e
		}
		if e = q.Post(ctx, walletsql.PostParams{Column1: op, Column2: []string{"platform:packages", "user:" + u.Actor()}, Column3: []int64{v.Price, -v.Price}}); e != nil {
			return e
		}

		out = packages.Purchase{AccountID: u.Actor(), CreatedAt: now.Time.UTC(), StartsAt: v.StartsAt, ExpiresAt: v.ExpiresAt, OperationID: op, PackageID: id, VersionID: v.VersionID, EntitlementID: entitlement, Price: v.Price, FinancialStatus: "committed"}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return q.SaveCommand(ctx, walletsql.SaveCommandParams{ActorID: u.Actor(), Kind: "package_purchase", IdempotencyKey: key, Fingerprint: fingerprint, Result: string(b)})
	})
	return out, safe(err)
}
