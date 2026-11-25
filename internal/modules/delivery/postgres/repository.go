// Package postgres owns nonfinancial catalog/media/push state and composes
// existing authorization, Wallet, Live and Profile transaction boundaries.
package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool       *pgxpool.Pool
	guard      Guard
	live       *livepg.Repository
	store      delivery.ObjectStore
	cfg        config.DeliveryConfig
	key        []byte
	processors chan struct{}
}

func New(pool *pgxpool.Pool, live *livepg.Repository, store delivery.ObjectStore, cfg config.DeliveryConfig) (*Repository, error) {
	g, e := accesspg.New(pool)
	if e != nil {
		return nil, e
	}
	key, e := base64.StdEncoding.DecodeString(cfg.TokenKey.Value())
	if e != nil || len(key) != 0 && len(key) != 32 {
		return nil, delivery.Fail(apperror.ServiceUnavailable)
	}
	if store == nil {
		store = delivery.UnavailableStore{}
	}
	if cfg.DatabaseTimeout == 0 {
		cfg.DatabaseTimeout = 8 * time.Second
	}
	return &Repository{pool, g, live, store, cfg, key, make(chan struct{}, 2)}, nil
}
func safe(e error) error {
	if e == nil {
		return nil
	}
	var public *apperror.Error
	if errors.As(e, &public) {
		return public
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return delivery.Fail(apperror.NotFound)
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23503") {
		return delivery.Fail(apperror.Conflict)
	}
	return delivery.Fail(apperror.ServiceUnavailable)
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t.UTC(), Valid: true} }
func (r *Repository) Ready(ctx context.Context) error {
	ok, e := deliverysql.New(r.pool).Ready(ctx)
	if e != nil || !ok.Valid || !ok.Bool {
		return delivery.Fail(apperror.ServiceUnavailable)
	}
	return nil
}
func (r *Repository) within(ctx context.Context, c access.Credentials, p string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.DatabaseTimeout)
	defer cancel()
	return safe(r.guard.Authorized(ctx, c, p, false, "", fn))
}
func audit(ctx context.Context, u access.Unit, target, action, reason string) error {
	id := requestmeta.ID(ctx)
	if id == "" {
		id = access.ID()
	}
	return u.Audit(ctx, access.Audit{Actor: u.Actor(), Target: target, Action: action, Fields: []string{"delivery"}, Reason: reason, Correlation: id})
}
func gift(v deliverysql.GiftRow) delivery.Gift {
	return delivery.Gift{ID: v.ID, Code: v.Code, VersionID: v.VersionID, Name: v.Name, Description: v.Description, Price: v.Price, Category: v.Category, ThumbnailAssetID: v.ThumbnailAssetID.String, ThumbnailURL: v.ThumbnailUrl, AnimationURL: v.AnimationUrl, Active: v.Active, DisplayOrder: v.DisplayOrder, CreatedAt: v.CreatedAt.Time.UTC(), UpdatedAt: v.UpdatedAt.Time.UTC()}
}
func asset(v deliverysql.AppDeliveryAsset) delivery.Asset {
	thumb := ""
	if v.PublicUrl != "" && v.ApprovedKey != "" && v.ThumbnailKey != "" {
		thumb = strings.TrimSuffix(v.PublicUrl, v.ApprovedKey) + v.ThumbnailKey
	}
	return delivery.Asset{ThumbnailURL: thumb, ID: v.ID, Purpose: v.Purpose, State: v.State, URL: v.PublicUrl, Width: v.Width, Height: v.Height, Size: v.SizeBytes, ErrorCode: v.ErrorCode, ExpiresAt: v.ExpiresAt.Time.UTC()}
}

type Page = delivery.Page

func ValidPage(cursor string, limit int) bool { return delivery.ValidPage(cursor, limit) }
func (r *Repository) command(ctx context.Context, c access.Credentials, permission, kind, key, reason string, input any, fn func(context.Context, pgx.Tx, access.Unit) (any, string, error)) (out any, err error) {
	if !access.ValidCorrelation(key) || len(key) > 64 || !access.ValidReason(reason) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	fingerprint, e := delivery.Fingerprint(input)
	if e != nil {
		return nil, safe(e)
	}
	err = r.within(ctx, c, permission, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		prior, e := q.Command(ctx, deliverysql.CommandParams{ActorID: u.Actor(), Kind: kind, Key: key})
		if e == nil {
			if prior.Fingerprint != fingerprint {
				return delivery.Fail(apperror.Conflict)
			}
			out = json.RawMessage(prior.Result)
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		var target string
		out, target, e = fn(ctx, tx, u)
		if e != nil {
			return e
		}
		if e = audit(ctx, u, target, kind, reason); e != nil {
			return e
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return q.SaveCommand(ctx, deliverysql.SaveCommandParams{ActorID: u.Actor(), Kind: kind, Key: key, Fingerprint: fingerprint, Result: string(b)})
	})
	return
}

type Guard interface {
	Authorized(context.Context, access.Credentials, string, bool, string, func(context.Context, pgx.Tx, access.Unit) error) error
}
