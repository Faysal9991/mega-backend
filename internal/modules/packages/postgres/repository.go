package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/modules/packages/postgres/packagessql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"slices"
	"time"
)

type Buyer interface {
	PurchasePackage(context.Context, access.Credentials, string, string) (packages.Purchase, error)
}
type Repository struct {
	guard *accesspg.Repository
	buyer Buyer
	pool  *pgxpool.Pool
}

func New(pool *pgxpool.Pool, b Buyer) (*Repository, error) {
	g, e := accesspg.New(pool)
	if e != nil {
		return nil, e
	}
	return &Repository{guard: g, buyer: b, pool: pool}, nil
}
func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
func definition(v packagessql.GetPackageRow) packages.Definition {
	return packages.Definition{ID: v.ID, Code: v.Code, Version: v.VersionID, Name: v.Name, Description: v.Description, Price: v.Price, Duration: v.DurationSeconds, Powers: v.Powers, Badge: v.BadgeAssetID.String, Entrance: v.EntranceAssetID.String, Priority: v.ListingPriority, Active: v.Active, CreatedAt: v.CreatedAt.Time.UTC()}
}
func safe(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return access.Failure(apperror.NotFound)
	}
	return access.Safe(e)
}
func (r *Repository) Purchase(ctx context.Context, c access.Credentials, id, key string) (packages.Purchase, error) {
	if !access.ValidID(id) || !access.ValidCorrelation(key) {
		return packages.Purchase{}, access.Failure(apperror.ValidationFailed)
	}
	if r.buyer == nil {
		return packages.Purchase{}, access.Failure(apperror.FinancialConfigurationRequired)
	}
	return r.buyer.PurchasePackage(ctx, c, id, key)
}
func (r *Repository) Execute(ctx context.Context, c access.Credentials, in packages.Command) (out any, err error) {
	if !access.ValidCorrelation(in.Key) || !access.ValidReason(in.Reason) || in.ID != "" && !access.ValidID(in.ID) || in.Target != "" && !access.ValidID(in.Target) {
		return nil, access.Failure(apperror.ValidationFailed)
	}
	switch in.Kind {
	case "save":
		if in.Target != "" {
			return nil, access.Failure(apperror.ValidationFailed)
		}
	case "grant":
		if !access.ValidID(in.ID) || !access.ValidID(in.Target) || in.Definition != nil {
			return nil, access.Failure(apperror.ValidationFailed)
		}
	case "revoke":
		if !access.ValidID(in.ID) || in.Target != "" || in.Definition != nil {
			return nil, access.Failure(apperror.ValidationFailed)
		}
	default:
		return nil, access.Failure(apperror.ValidationFailed)
	}
	if in.Kind == "save" {
		if in.Definition == nil {
			return nil, access.Failure(apperror.ValidationFailed)
		}
		if e := in.Definition.Validate(); e != nil {
			return nil, e
		}
	}
	if in.Definition != nil {
		d := *in.Definition
		d.Powers = slices.Clone(d.Powers)
		slices.Sort(d.Powers)
		d.ID = ""
		d.Version = ""
		d.CreatedAt = time.Time{}
		in.Definition = &d
	}
	permission := "packages.manage"
	if in.Kind == "grant" || in.Kind == "revoke" {
		permission = "packages.entitlements.manage"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fingerprint, e := live.Fingerprint(in)
	if e != nil {
		return nil, e
	}
	err = r.guard.Authorized(ctx, c, permission, false, in.Target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := packagessql.New(tx)
		prior, e := q.ExistingCommand(ctx, packagessql.ExistingCommandParams{ActorID: u.Actor(), Kind: in.Kind, Key: in.Key})
		if e == nil {
			if prior.Fingerprint != fingerprint {
				return access.Failure(apperror.Conflict)
			}
			return json.Unmarshal(prior.Result, &out)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		switch in.Kind {
		case "save":
			d := *in.Definition
			d.ID = in.ID
			if d.ID == "" {
				d.ID = access.ID()
				if d.Previous != "" {
					return access.Failure(apperror.Conflict)
				}
				if e = q.CreatePackage(ctx, packagessql.CreatePackageParams{ID: d.ID, Code: d.Code}); e != nil {
					return e
				}
			} else {
				old, e := q.GetPackage(ctx, d.ID)
				if e != nil {
					return e
				}
				if old.Code != d.Code || old.VersionID != d.Previous {
					return access.Failure(apperror.Conflict)
				}
			}
			for _, id := range []string{d.Badge, d.Entrance} {
				if id != "" {
					var ok bool
					if ok, e = q.UsablePackageAsset(ctx, id); e != nil {
						return e
					}
					if !ok {
						return access.Failure(apperror.Conflict)
					}
				}
			}
			d.Version = access.ID()
			if e = q.InsertVersion(ctx, packagessql.InsertVersionParams{ID: d.Version, PackageID: d.ID, Name: d.Name, Description: d.Description, Price: d.Price, DurationSeconds: d.Duration, Powers: d.Powers, BadgeAssetID: text(d.Badge), EntranceAssetID: text(d.Entrance), ListingPriority: d.Priority, Active: d.Active}); e != nil {
				return e
			}
			if _, e = q.SwitchVersion(ctx, packagessql.SwitchVersionParams{ID: d.ID, CurrentVersion: text(d.Version), Previous: text(d.Previous)}); e != nil {
				return access.Failure(apperror.Conflict)
			}
			stored, e := q.GetPackage(ctx, d.ID)
			if e != nil {
				return e
			}
			result := definition(stored)
			result.Previous = d.Previous
			out = result
		case "grant":
			n, e := q.ActiveEntitlementCount(ctx, in.Target)
			if e != nil {
				return e
			}
			if n >= 100 {
				return access.Failure(apperror.Conflict)
			}
			p, e := q.GetPackage(ctx, in.ID)
			if e != nil {
				return e
			}
			if !p.Active {
				return access.Failure(apperror.Conflict)
			}
			now, e := q.Clock(ctx)
			if e != nil {
				return e
			}
			id := access.ID()
			expires := now.Time.Add(time.Duration(p.DurationSeconds) * time.Second)
			if e = q.InsertEntitlement(ctx, packagessql.InsertEntitlementParams{ID: id, AccountID: in.Target, PackageID: in.ID, StartsAt: now, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, Source: "direct"}); e != nil {
				return e
			}
			out = packages.Entitlement{ID: id, AccountID: in.Target, PackageID: in.ID, StartsAt: now.Time, ExpiresAt: expires, Source: "direct", Powers: p.Powers}
		case "revoke":
			if _, e = q.RevokeEntitlement(ctx, packagessql.RevokeEntitlementParams{ID: in.ID, ActorID: u.Actor()}); e != nil {
				return e
			}
			out = map[string]string{"entitlement_id": in.ID, "status": "revoked"}
		default:
			return access.Failure(apperror.ValidationFailed)
		}
		auditTarget := in.ID
		switch result := out.(type) {
		case packages.Definition:
			auditTarget = result.ID
		case packages.Entitlement:
			auditTarget = result.ID
		}
		if e = u.Audit(ctx, access.Audit{Actor: u.Actor(), Target: auditTarget, Action: "packages." + in.Kind, Reason: in.Reason, Correlation: in.Key, Fields: []string{"command:" + fingerprint}}); e != nil {
			return e
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return q.SaveCommand(ctx, packagessql.SaveCommandParams{ActorID: u.Actor(), Kind: in.Kind, Key: in.Key, Fingerprint: fingerprint, Result: string(b)})
	})
	return out, safe(err)
}

func (r *Repository) Ready(ctx context.Context) error {
	ok, e := packagessql.New(r.pool).Ready(ctx, int64(len(access.Catalog())))
	if e != nil || !ok.Valid || !ok.Bool {
		return access.Failure(apperror.ServiceUnavailable)
	}
	return nil
}
