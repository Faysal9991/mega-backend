package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Register(ctx context.Context, c access.Credentials, platform, token string) error {
	return r.RegisterWithPrevious(ctx, c, platform, token, "")
}
func (r *Repository) RegisterWithPrevious(ctx context.Context, c access.Credentials, platform, token, previous string) error {
	if previous != "" && len(previous) != 43 {
		return delivery.Fail(apperror.ValidationFailed)
	}
	for _, b := range []byte(previous) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return delivery.Fail(apperror.ValidationFailed)
		}
	}
	if platform != "android" && platform != "ios" || len(token) < 16 || len(token) > 4096 || !delivery.Text(token, 4096) || strings.ContainsAny(token, " \t\n\r") {
		return delivery.Fail(apperror.ValidationFailed)
	}
	for _, runeValue := range token {
		if unicode.IsSpace(runeValue) {
			return delivery.Fail(apperror.ValidationFailed)
		}
	}
	if len(r.key) != 32 {
		return delivery.Fail(apperror.ServiceUnavailable)
	}
	return r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		session, e := q.CurrentSession(ctx, deliverysql.CurrentSessionParams{AccountID: u.Actor(), InstallationID: c.Installation, AccessHash: identity.Digest(c.Access)})
		if e != nil {
			return e
		}
		if e = q.UnbindInactiveInstallation(ctx, c.Installation); e != nil {
			return e
		}
		prior, e := q.Device(ctx, c.Installation)
		if errors.Is(e, pgx.ErrNoRows) {
			n, e := q.Devices(ctx, u.Actor())
			if e != nil {
				return e
			}
			if n >= 10 {
				return delivery.Fail(apperror.RateLimited)
			}
		} else if e != nil {
			return e
		} else if prior.AccountID != u.Actor() {
			if previous == "" {
				return delivery.Fail(apperror.Conflict)
			}
			proof, e := identitypg.BindSecurity(tx).Access(ctx, identity.Digest(previous), c.Installation)
			if e != nil && !errors.Is(e, identity.ErrMissing) {
				return e
			}
			if e != nil || proof.Account.ID != prior.AccountID || proof.SessionID != prior.SessionID {
				return delivery.Fail(apperror.Forbidden)
			}
			n, e := q.Devices(ctx, u.Actor())
			if e != nil {
				return e
			}
			if n >= 10 {
				return delivery.Fail(apperror.RateLimited)
			}
			if e = q.UnbindInstallation(ctx, c.Installation); e != nil {
				return e
			}
			if e = audit(ctx, u, prior.AccountID, "push.binding.switch", "Authenticated push account switch"); e != nil {
				return e
			}
		}
		cipher, e := delivery.Seal(r.key, token, c.Installation)
		if e != nil {
			return e
		}
		return q.RegisterDevice(ctx, deliverysql.RegisterDeviceParams{InstallationID: c.Installation, AccountID: u.Actor(), SessionID: session, Platform: platform, TokenHash: delivery.Hash(token), TokenCipher: cipher})
	})
}
func (r *Repository) RemoveDevice(ctx context.Context, c access.Credentials) error {
	return r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		return deliverysql.New(tx).RemoveDevice(ctx, deliverysql.RemoveDeviceParams{InstallationID: c.Installation, AccountID: u.Actor()})
	})
}
func prefs(v deliverysql.AppNotificationPreference) delivery.Preferences {
	return delivery.Preferences{GiftReceived: v.GiftReceived, AgencyDecision: v.AgencyDecision, AccountEvent: v.AccountEvent, RoomInvitation: v.RoomInvitation, AdminAnnouncement: v.AdminAnnouncement}
}
func (r *Repository) Preferences(ctx context.Context, c access.Credentials, in *delivery.Preferences) (out delivery.Preferences, err error) {
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		if in != nil {
			if e := q.PutPreferences(ctx, deliverysql.PutPreferencesParams{AccountID: u.Actor(), GiftReceived: in.GiftReceived, AgencyDecision: in.AgencyDecision, AccountEvent: in.AccountEvent, RoomInvitation: in.RoomInvitation, AdminAnnouncement: in.AdminAnnouncement}); e != nil {
				return e
			}
		}
		v, e := q.Preferences(ctx, u.Actor())
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		out = prefs(v)
		return e
	})
	return
}
func (r *Repository) NotificationHistory(ctx context.Context, c access.Credentials, cursor string, limit int) (out Page, err error) {
	if !ValidPage(cursor, limit) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	out.Limit = limit
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		rows, e := deliverysql.New(tx).NotificationHistory(ctx, deliverysql.NotificationHistoryParams{AccountID: u.Actor(), ID: cursor, Limit: int32(limit + 1)})
		if e != nil {
			return e
		}
		items := []any{}
		for i, v := range rows {
			if i == limit {
				out.More = true
				break
			}
			items = append(items, map[string]any{"notification_id": v.ID, "type": v.Kind, "reference": v.Reference, "template": v.Template, "created_at": v.CreatedAt.Time.UTC()})
			out.Next = v.ID
		}
		out.Data = items
		return nil
	})
	return
}

type Announcement = delivery.Announcement

func (r *Repository) Announcement(ctx context.Context, c access.Credentials, key string, in Announcement) (any, error) {
	if len(in.Accounts) < 1 || len(in.Accounts) > 100 || !slices.Contains([]string{"maintenance", "service_update", "community_update"}, in.Template) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	for _, id := range in.Accounts {
		if !access.ValidID(id) {
			return nil, delivery.Fail(apperror.ValidationFailed)
		}
	}
	in.Accounts = slices.Clone(in.Accounts)
	slices.Sort(in.Accounts)
	in.Accounts = slices.Compact(in.Accounts)
	return r.command(ctx, c, delivery.Announce, "notification.announce", key, in.Reason, in, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		q := deliverysql.New(tx)
		id := access.ID()
		if e := q.CreateAnnouncement(ctx, deliverysql.CreateAnnouncementParams{ID: id, Template: in.Template}); e != nil {
			return nil, "", e
		}
		rows, e := q.NotifyMany(ctx, deliverysql.NotifyManyParams{Reference: id, Targets: in.Accounts})
		if e != nil {
			return nil, "", e
		}
		notificationIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			notificationIDs = append(notificationIDs, row.ID)
		}
		if e = q.QueueManyPush(ctx, notificationIDs); e != nil {
			return nil, "", e
		}
		return map[string]any{"announcement_id": id, "accepted_accounts": len(rows), "delivery_status": "pending"}, id, nil
	})
}
func (r *Repository) Deliveries(ctx context.Context, c access.Credentials, cursor string, limit int) (out Page, err error) {
	if !ValidPage(cursor, limit) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	out.Limit = limit
	err = r.within(ctx, c, delivery.Inspect, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		rows, e := deliverysql.New(tx).Deliveries(ctx, deliverysql.DeliveriesParams{ID: cursor, Limit: int32(limit + 1)})
		if e != nil {
			return e
		}
		items := []any{}
		for i, v := range rows {
			if i == limit {
				out.More = true
				break
			}
			items = append(items, map[string]any{"delivery_id": v.ID, "topic": v.Topic, "aggregate_id": v.AggregateID, "state": v.State, "attempts": v.Attempts, "error_code": v.ErrorCode, "created_at": v.CreatedAt.Time.UTC(), "updated_at": v.UpdatedAt.Time.UTC()})
			out.Next = v.ID
		}
		out.Data = items
		return nil
	})
	return
}

func (r *Repository) Retry(ctx context.Context, c access.Credentials, id, key, reason string) (any, error) {
	if !access.ValidID(id) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	return r.command(ctx, c, "deliveries.retry", "delivery.retry", key, reason, []string{id, reason}, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		n, e := deliverysql.New(tx).RetryDelivery(ctx, id)
		if e != nil {
			return nil, "", e
		}
		if n != 1 {
			return nil, "", delivery.Fail(apperror.Conflict)
		}
		return map[string]string{"delivery_id": id, "state": "pending"}, id, nil
	})
}
