package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/intents"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	walletpg "github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) Gifts(ctx context.Context, c access.Credentials, id, cursor string, limit int, admin bool) (out Page, err error) {
	if !ValidPage(cursor, limit) || id != "" && !access.ValidID(id) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	p := ""
	if admin {
		p = delivery.GiftsManage
	}
	out.Limit = limit
	err = r.within(ctx, c, p, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		if id != "" {
			v, e := q.Gift(ctx, id)
			if e != nil {
				return e
			}
			if !admin && !v.Active {
				return delivery.Fail(apperror.NotFound)
			}
			out.Data = gift(v)
			return nil
		}
		rows, e := q.Gifts(ctx, deliverysql.GiftsParams{Cursor: cursor, PageLimit: int32(limit + 1), Admin: admin})
		if e != nil {
			return e
		}
		items := []delivery.Gift{}
		for i, v := range rows {
			if i == limit {
				out.More = true
				break
			}
			items = append(items, gift(deliverysql.GiftRow(v)))
			out.Next = v.ID
		}
		out.Data = items
		return nil
	})
	return
}

type GiftChange = delivery.GiftChange

func (r *Repository) PutGift(ctx context.Context, c access.Credentials, id, key string, in GiftChange) (any, error) {
	if e := in.Gift.Validate(); e != nil {
		return nil, e
	}
	if id != "" && !access.ValidID(id) || in.PreviousID != "" && !access.ValidID(in.PreviousID) || !delivery.AnimationAllowed(in.Gift.AnimationURL, r.cfg.AnimationHosts) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	return r.command(ctx, c, delivery.GiftsManage, "gift.catalog", key, in.Reason, struct {
		ID    string
		Input GiftChange
	}{id, in}, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		q := deliverysql.New(tx)
		g := in.Gift
		g.ID = id
		if id == "" {
			if in.PreviousID != "" {
				return nil, "", delivery.Fail(apperror.Conflict)
			}
			g.ID = access.ID()
			if e := q.CreateGift(ctx, deliverysql.CreateGiftParams{ID: g.ID, Code: g.Code}); e != nil {
				return nil, "", e
			}
		} else {
			prior, e := q.Gift(ctx, id)
			if e != nil {
				return nil, "", e
			}
			if prior.VersionID != in.PreviousID || prior.Code != g.Code {
				return nil, "", delivery.Fail(apperror.Conflict)
			}
		}
		var thumb pgtype.Text
		g.ThumbnailURL = ""
		if g.ThumbnailAssetID != "" {
			v, e := q.Asset(ctx, g.ThumbnailAssetID)
			if e != nil {
				return nil, "", e
			}
			if v.State != "ready" || v.Purpose != "gift_thumbnail" {
				return nil, "", delivery.Fail(apperror.Forbidden)
			}
			thumb = pgtype.Text{String: v.ID, Valid: true}
			g.ThumbnailURL = asset(v).ThumbnailURL
		}
		g.VersionID = access.ID()
		if e := q.VersionGift(ctx, deliverysql.VersionGiftParams{ID: g.VersionID, GiftID: g.ID, Name: g.Name, Description: g.Description, Price: g.Price, Category: g.Category, ThumbnailAssetID: thumb, ThumbnailUrl: g.ThumbnailURL, AnimationUrl: g.AnimationURL, Active: g.Active, DisplayOrder: g.DisplayOrder}); e != nil {
			return nil, "", e
		}
		if e := q.PointGift(ctx, deliverysql.PointGiftParams{GiftID: g.ID, VersionID: g.VersionID}); e != nil {
			return nil, "", e
		}
		v, e := q.Gift(ctx, g.ID)
		return gift(v), g.ID, e
	})
}
func (r *Repository) RecipientPolicy(ctx context.Context, c access.Credentials, key, previous, reason string) (any, error) {
	if previous != "" && !access.ValidID(previous) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	return r.command(ctx, c, delivery.GiftsManage, "gift.recipient_policy", key, reason, []string{previous, reason}, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		q := deliverysql.New(tx)
		prior, e := q.RecipientPolicy(ctx)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, "", e
		}
		if e == nil && prior.ID != previous || errors.Is(e, pgx.ErrNoRows) && previous != "" {
			return nil, "", delivery.Fail(apperror.Conflict)
		}
		id := access.ID()
		e = q.PublishRecipientPolicy(ctx, deliverysql.PublishRecipientPolicyParams{ID: id, PreviousID: previous})
		return map[string]string{"policy_id": id, "mode": "active_publishers"}, id, e
	})
}
func (r *Repository) Resolve(ctx context.Context, tx pgx.Tx, actor string, in wallet.Command) (walletpg.GiftCatalogSnapshot, error) {
	q := deliverysql.New(tx)
	if _, e := q.RecipientPolicy(ctx); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return walletpg.GiftCatalogSnapshot{}, wallet.Error(wallet.ConfigurationRequired)
		}
		return walletpg.GiftCatalogSnapshot{}, e
	}
	if r.live == nil {
		return walletpg.GiftCatalogSnapshot{}, wallet.Error(wallet.ConfigurationRequired)
	}
	if e := r.live.ValidateGift(ctx, tx, in.RoomID, in.SessionID, actor, in.Target); e != nil {
		return walletpg.GiftCatalogSnapshot{}, e
	}
	v, e := q.Gift(ctx, in.GiftID)
	if e != nil {
		return walletpg.GiftCatalogSnapshot{}, e
	}
	if !v.Active {
		return walletpg.GiftCatalogSnapshot{}, delivery.Fail(apperror.NotFound)
	}
	raw, e := json.Marshal(gift(v))
	return walletpg.GiftCatalogSnapshot{Price: v.Price, Snapshot: raw}, e
}
func (r *Repository) Record(ctx context.Context, tx pgx.Tx, id, actor string, in wallet.Command, total int64, snapshot walletpg.GiftCatalogSnapshot) error {
	q := deliverysql.New(tx)
	var g delivery.Gift
	if e := json.Unmarshal(snapshot.Snapshot, &g); e != nil {
		return e
	}
	names, e := q.PublicNames(ctx, []string{actor, in.Target})
	if e != nil {
		return e
	}
	public := func(id string) map[string]string {
		name := ""
		for _, p := range names {
			if p.AccountID == id {
				name = p.DisplayName
			}
		}
		for len(name) > 256 {
			name = name[:len(name)-1]
			for !utf8.ValidString(name) {
				name = name[:len(name)-1]
			}
		}
		return map[string]string{"account_id": id, "display_name": name}
	}
	now, e := q.Clock(ctx)
	if e != nil {
		return e
	}
	event := map[string]any{"event_id": id, "type": "room.gift.v1", "operation_id": id, "room_id": in.RoomID, "session_id": in.SessionID, "sender": public(actor), "recipient": public(in.Target), "gift_id": g.ID, "gift_version_id": g.VersionID, "quantity": in.Quantity, "committed_diamonds": total, "animation_url": g.AnimationURL, "created_at": now.Time.UTC()}
	b, e := json.Marshal(event)
	if e != nil || len(b) > 4096 {
		return delivery.Fail(apperror.ValidationFailed)
	}
	if e = q.RecordGift(ctx, deliverysql.RecordGiftParams{OperationID: id, GiftID: g.ID, VersionID: g.VersionID, RoomID: in.RoomID, SessionID: in.SessionID, SenderID: actor, RecipientID: in.Target, Event: string(b)}); e != nil {
		return e
	}
	if e = intents.Enqueue(ctx, tx, id, "gift", id, event, now.Time); e != nil {
		return e
	}
	return intents.Notify(ctx, tx, in.Target, "gift_received", id)
}
func (r *Repository) GiftHistory(ctx context.Context, c access.Credentials, cursor string, limit int, admin bool) (out Page, err error) {
	if !ValidPage(cursor, limit) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	p := ""
	if admin {
		p = delivery.Inspect
	}
	out.Limit = limit
	err = r.within(ctx, c, p, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		rows, e := deliverysql.New(tx).GiftHistory(ctx, deliverysql.GiftHistoryParams{OperationID: cursor, SenderID: u.Actor(), Admin: admin, Limit: int32(limit + 1)})
		if e != nil {
			return e
		}
		items := []any{}
		for i, v := range rows {
			if i == limit {
				out.More = true
				break
			}
			items = append(items, map[string]any{"event": json.RawMessage(v.Event), "financial_status": "committed", "delivery_status": v.DeliveryState})
			out.Next = v.OperationID
		}
		out.Data = items
		return nil
	})
	return
}
