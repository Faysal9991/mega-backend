// Package activation owns purchased-entitlement writes inside Wallet's transaction.
package activation

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/packages/postgres/packagessql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type Offer struct {
	PackageID, VersionID   string
	Price, DurationSeconds int64
	StartsAt, ExpiresAt    time.Time
}

func Resolve(ctx context.Context, db packagessql.DBTX, actor, id string, now time.Time) (Offer, error) {
	q := packagessql.New(db)
	v, e := q.GetPackage(ctx, id)
	if e != nil {
		return Offer{}, e
	}
	if !v.Active || v.Price <= 0 || v.DurationSeconds < 60 {
		return Offer{}, access.Failure(apperror.FinancialConfigurationRequired)
	}
	n, e := q.ActiveEntitlementCount(ctx, actor)
	if e != nil {
		return Offer{}, e
	}
	if n >= 100 {
		return Offer{}, access.Failure(apperror.Conflict)
	}
	start, e := q.RenewalStart(ctx, packagessql.RenewalStartParams{AccountID: actor, PackageID: id, Now: pgtype.Timestamptz{Time: now, Valid: true}})
	if e != nil {
		return Offer{}, e
	}
	expires := start.Time.Add(time.Duration(v.DurationSeconds) * time.Second)
	if !start.Valid || start.Time.Before(now) || expires.Year() > 9999 {
		return Offer{}, access.Failure(apperror.FinancialConfigurationRequired)
	}
	return Offer{PackageID: v.ID, VersionID: v.VersionID, Price: v.Price, DurationSeconds: v.DurationSeconds, StartsAt: start.Time.UTC(), ExpiresAt: expires.UTC()}, nil
}
func Record(ctx context.Context, db packagessql.DBTX, actor, operation, entitlement string, offer Offer, now time.Time) error {
	q := packagessql.New(db)
	if e := q.InsertEntitlement(ctx, packagessql.InsertEntitlementParams{ID: entitlement, AccountID: actor, PackageID: offer.PackageID, StartsAt: pgtype.Timestamptz{Time: offer.StartsAt, Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: offer.ExpiresAt, Valid: true}, Source: "purchase"}); e != nil {
		return e
	}
	return q.InsertPurchase(ctx, packagessql.InsertPurchaseParams{OperationID: operation, AccountID: actor, PackageID: offer.PackageID, VersionID: offer.VersionID, EntitlementID: entitlement, Price: offer.Price, CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}})
}
