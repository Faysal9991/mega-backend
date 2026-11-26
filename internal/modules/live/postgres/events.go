package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Ingest(ctx context.Context, event live.Event) error {
	if event.Type != "participant_joined" && event.Type != "participant_left" && event.Type != "room_finished" && event.Type != "room_started" {
		return nil
	}
	if !live.Text(event.ID, 128) || !access.ValidID(event.SessionID) || !live.Text(event.RoomSID, 128) || event.At.IsZero() {
		return live.Fail(apperror.ValidationFailed)
	}
	if event.Type == "participant_joined" || event.Type == "participant_left" {
		if !live.Text(event.AccountID, 128) || !live.Text(event.ParticipantSID, 128) {
			return live.Fail(apperror.ValidationFailed)
		}
	}
	fingerprint, e := live.Fingerprint(event)
	if e != nil {
		return safe(e)
	}
	ctx, cancel := context.WithTimeout(ctx, r.limits.DatabaseTimeout)
	defer cancel()
	return safe(r.tx.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if e := accesssql.New(tx).LockSecurity(ctx); e != nil {
			return e
		}
		q := livesql.New(tx)
		limits, e := r.effective(ctx, q)
		if e != nil {
			return e
		}
		old, e := q.Event(ctx, event.ID)
		if e == nil {
			if old != fingerprint {
				return live.Fail(apperror.Conflict)
			}
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		now, e := q.Clock(ctx)
		if e != nil {
			return e
		}
		if event.At.Before(now.Time.Add(-r.limits.EventAge)) || event.At.After(now.Time.Add(5*time.Second)) {
			return live.Fail(apperror.Conflict)
		}
		v, e := q.SessionRoom(ctx, event.SessionID)
		if e != nil {
			return e
		}
		if v.ProviderSid != "" && v.ProviderSid != event.RoomSID {
			if e := q.RecordEvent(ctx, livesql.RecordEventParams{ID: event.ID, Fingerprint: fingerprint, EventType: event.Type, RoomID: v.ID, ParticipantSid: event.ParticipantSID, ProviderAt: ts(event.At)}); e != nil {
				return e
			}
			if e := closeVerifiedRoom(ctx, q, v.ID); e != nil {
				return e
			}
			return q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.ID, Kind: "close"})
		}
		if _, e := q.BindProviderSID(ctx, livesql.BindProviderSIDParams{ID: v.ID, ProviderSid: event.RoomSID}); e != nil {
			return e
		}
		if e := q.RecordEvent(ctx, livesql.RecordEventParams{ID: event.ID, Fingerprint: fingerprint, EventType: event.Type, RoomID: v.ID, ParticipantSid: event.ParticipantSID, ProviderAt: ts(event.At)}); e != nil {
			return e
		}
		switch event.Type {
		case "room_started":
			return nil // Only the durable create operation opens business admission.
		case "room_finished":
			return closeVerifiedRoom(ctx, q, v.ID)
		}
		known, ke := q.AccountActive(ctx, event.AccountID)
		if ke != nil && !errors.Is(ke, pgx.ErrNoRows) {
			return ke
		}
		if errors.Is(ke, pgx.ErrNoRows) || !access.ValidID(event.AccountID) {
			if event.Type == "participant_joined" {
				return q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.ID, Kind: "participant", Target: event.AccountID})
			}
			return nil
		}
		_ = known
		prior, pe := q.Presence(ctx, event.ParticipantSID)
		if pe != nil && !errors.Is(pe, pgx.ErrNoRows) {
			return pe
		}
		if pe == nil && (prior.RoomID != v.ID || prior.AccountID != event.AccountID) {
			return live.Fail(apperror.Conflict)
		}
		if event.Type == "participant_left" {
			if errors.Is(pe, pgx.ErrNoRows) {
				return q.EndTombstone(ctx, livesql.EndTombstoneParams{ParticipantSid: event.ParticipantSID, RoomID: v.ID, AccountID: event.AccountID, EndedAt: ts(event.At)})
			}
			if prior.EndedAt.Valid {
				return nil
			}
			end := event.At
			if end.After(now.Time) {
				end = now.Time
			}
			if end.Before(prior.StartedAt.Time) {
				end = prior.StartedAt.Time
			}
			return q.EndPresence(ctx, livesql.EndPresenceParams{ParticipantSid: event.ParticipantSID, EndedAt: ts(end)})
		}
		if pe == nil {
			return nil
		} // A left-first tombstone cannot be reopened by delayed joins.
		m, me := q.Member(ctx, livesql.MemberParams{RoomID: v.ID, AccountID: event.AccountID})
		if me != nil && !errors.Is(me, pgx.ErrNoRows) {
			return me
		}
		if me != nil || m.State != "active" || v.State != "open" {
			if e := q.EndTombstone(ctx, livesql.EndTombstoneParams{ParticipantSid: event.ParticipantSID, RoomID: v.ID, AccountID: event.AccountID, EndedAt: now}); e != nil {
				return e
			}
			// Unknown identities are removed through durable server-authored intent.
			return q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.ID, Kind: "participant", Target: event.AccountID})
		}
		if m.Hidden || m.SelfSeated || m.PrivateEntry {
			effective, e := accesspg.Resolve(ctx, tx, event.AccountID, v.ID)
			if e != nil {
				return e
			}
			if !effective.Allows("room.enter") || m.Hidden && !effective.Allows("room.join.hidden") || m.SelfSeated && !effective.Allows("room.seat.self_join") {
				if e = q.EndTombstone(ctx, livesql.EndTombstoneParams{ParticipantSid: event.ParticipantSID, RoomID: v.ID, AccountID: event.AccountID, EndedAt: now}); e != nil {
					return e
				}
				return q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.ID, Kind: "participant", Target: event.AccountID})
			}
		}
		aid, classification := "", ""
		qualifies := false
		if live.CanPublish(room(v), member(m)) {
			a, e := eligible(ctx, q, tx, event.AccountID, v.Kind, limits)
			if e == nil {
				aid = a.ID
				classification = a.Classification
				qualifies = true
			} else {
				var public *apperror.Error
				if !errors.As(e, &public) && !errors.Is(e, pgx.ErrNoRows) {
					return e
				}
			}
		}
		// Receipt time is a conservative server-owned start. Delayed delivery never
		// backfills qualifying time; provider disconnect time bounds the end.
		if e := q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.ID, Kind: "participant", Target: event.AccountID}); e != nil {
			return e
		}
		return q.StartPresence(ctx, livesql.StartPresenceParams{ParticipantSid: event.ParticipantSID, RoomID: v.ID, AccountID: event.AccountID, AssociationID: aid, Classification: classification, StartedAt: now, Qualifying: qualifies})
	}))
}

func closeVerifiedRoom(ctx context.Context, q *livesql.Queries, id string) error {
	if e := q.RoomState(ctx, livesql.RoomStateParams{ID: id, State: "closed"}); e != nil {
		return e
	}
	if e := q.CloseMembers(ctx, id); e != nil {
		return e
	}
	if e := q.InvalidateRoomPresence(ctx, id); e != nil {
		return e
	}
	open, e := q.OpenPresence(ctx, id)
	if e != nil {
		return e
	}
	ids := []string{}
	for _, v := range open {
		ids = append(ids, v.ParticipantSid)
	}
	return q.AbandonPresences(ctx, ids)
}
