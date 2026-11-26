package postgres

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/modules/packages/postgres/packagessql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"time"
)

func (r *Repository) Read(ctx context.Context, c access.Credentials, in packages.Query) (out packages.Page, err error) {
	if in.Limit < 1 || in.Limit > 50 || in.Cursor != "" && !access.ValidID(in.Cursor) || in.ID != "" && !access.ValidID(in.ID) {
		return out, access.Failure(apperror.ValidationFailed)
	}
	permission := ""
	admin := false
	switch in.Kind {
	case "admin.packages", "history", "subscribers":
		permission = "packages.manage"
		admin = true
	case "purchases":
		permission = "packages.purchases.read"
		admin = true
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out.Limit = in.Limit
	err = r.guard.Authorized(ctx, c, permission, false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := packagessql.New(tx)
		limit := int32(in.Limit + 1)
		switch in.Kind {
		case "packages", "admin.packages":
			rows, e := q.Packages(ctx, packagessql.PackagesParams{ID: in.Cursor, Admin: admin, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []packages.Definition{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, definition(packagessql.GetPackageRow(v)))
				out.Next = v.ID
			}
			out.Items = items
		case "package":
			v, e := q.GetPackage(ctx, in.ID)
			if e != nil {
				return e
			}
			if !v.Active {
				return access.Failure(apperror.NotFound)
			}
			out.Items = definition(v)
		case "entitlements":
			rows, e := q.Entitlements(ctx, packagessql.EntitlementsParams{AccountID: u.Actor(), ID: in.Cursor, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []packages.Entitlement{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, packages.Entitlement{ID: v.ID, AccountID: v.AccountID, PackageID: v.PackageID, StartsAt: v.StartsAt.Time, ExpiresAt: v.ExpiresAt.Time, Source: v.Source, Powers: v.Powers})
				out.Next = v.ID
			}
			out.Items = items
		case "history":
			current, e := q.GetPackage(ctx, in.ID)
			if e != nil {
				return e
			}
			rows, e := q.VersionHistory(ctx, packagessql.VersionHistoryParams{PackageID: in.ID, ID: in.Cursor, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []packages.Definition{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, packages.Definition{ID: v.PackageID, Code: current.Code, Version: v.ID, Name: v.Name, Description: v.Description, Price: v.Price, Duration: v.DurationSeconds, Powers: v.Powers, Badge: v.BadgeAssetID.String, Entrance: v.EntranceAssetID.String, Priority: v.ListingPriority, Active: v.Active, CreatedAt: v.CreatedAt.Time.UTC()})
				out.Next = v.ID
			}
			out.Items = items
		case "subscribers":
			rows, e := q.Subscribers(ctx, packagessql.SubscribersParams{PackageID: in.ID, ID: in.Cursor, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []packages.Entitlement{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, packages.Entitlement{ID: v.ID, AccountID: v.AccountID, PackageID: v.PackageID, StartsAt: v.StartsAt.Time, ExpiresAt: v.ExpiresAt.Time, Source: v.Source, Powers: []string{}})
				out.Next = v.ID
			}
			out.Items = items
		case "purchases", "my.purchases":
			rows, e := q.Purchases(ctx, packagessql.PurchasesParams{AccountID: u.Actor(), Admin: admin, Cursor: in.Cursor, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []packages.Purchase{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, packages.Purchase{AccountID: v.AccountID, CreatedAt: v.CreatedAt.Time.UTC(), StartsAt: v.StartsAt.Time.UTC(), ExpiresAt: v.ExpiresAt.Time.UTC(), OperationID: v.OperationID, PackageID: v.PackageID, VersionID: v.VersionID, EntitlementID: v.EntitlementID, Price: v.Price, FinancialStatus: "committed"})
				out.Next = v.OperationID
			}
			out.Items = items
		default:
			return access.Failure(apperror.ValidationFailed)
		}
		return nil
	})
	return out, safe(err)
}
