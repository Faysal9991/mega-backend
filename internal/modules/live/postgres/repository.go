// Package postgres persists live business authority and durable provider intent.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Guard interface {
	Authorized(context.Context, access.Credentials, string, bool, string, func(context.Context, pgx.Tx, access.Unit) error) error
}
type Repository struct {
	pool   *pgxpool.Pool
	guard  Guard
	tx     *platform.Transactions
	limits live.Limits
}

func New(pool *pgxpool.Pool, l live.Limits) (*Repository, error) {
	if l.DatabaseTimeout == 0 {
		l.DatabaseTimeout = 8 * time.Second
	}
	if e := l.Validate(); e != nil {
		return nil, e
	}
	g, e := accesspg.New(pool)
	if e != nil {
		return nil, e
	}
	tx, e := platform.NewTransactions(pool, time.Second)
	if e != nil {
		return nil, e
	}
	return &Repository{pool, g, tx, l}, nil
}
func safe(e error) error {
	if e == nil {
		return nil
	}
	var a *apperror.Error
	if errors.As(e, &a) {
		return a
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return live.Fail(apperror.NotFound)
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "23505" || p.Code == "23503") {
		return live.Fail(apperror.Conflict)
	}
	return apperror.New(apperror.ServiceUnavailable, e)
}
func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t.UTC(), Valid: true} }
func optional(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}
func room(v livesql.AppLiveRoom) live.Room {
	return live.Room{ID: v.ID, SessionID: v.SessionID, OwnerID: v.OwnerID, Kind: v.Kind, Title: v.Title, State: v.State, Capacity: v.Capacity, Seats: v.Seats, CreatedAt: v.CreatedAt.Time.UTC(), ClosedAt: optional(v.ClosedAt), Visibility: v.Visibility, Announcement: v.Announcement, SeatsLocked: v.SeatsLocked}
}
func member(v livesql.AppLiveMember) live.Member {
	return live.Member{AccountID: v.AccountID, Role: v.Role, State: v.State, Muted: v.Muted, Seat: v.Seat.Int32, InvitedSeat: v.InvitedSeat.Int32, Hidden: v.Hidden, SelfSeated: v.SelfSeated, PrivateEntry: v.PrivateEntry}
}
func ag(v livesql.AppAgency) live.Agency {
	return live.Agency{ID: v.ID, Name: v.Name, Status: v.Status, CreatedAt: v.CreatedAt.Time.UTC()}
}
func application(v livesql.AppHostApplication) live.Application {
	return live.Application{ID: v.ID, AgencyID: v.AgencyID, AccountID: v.AccountID, Classification: v.Classification, State: v.State, CreatedAt: v.CreatedAt.Time.UTC(), DecidedAt: optional(v.DecidedAt)}
}
func association(v livesql.AppAgencyHostAssociation) live.Association {
	return live.Association{ID: v.ID, AccountID: v.AccountID, AgencyID: v.AgencyID, Classification: v.Classification, EffectiveFrom: v.EffectiveFrom.Time.UTC(), EffectiveUntil: optional(v.EffectiveUntil)}
}
func has(u access.Unit, p string) bool { return slices.Contains(u.Effective().Permissions, p) }
func manager(ctx context.Context, q *livesql.Queries, u access.Unit, id string) error {
	if id == u.Actor() || has(u, live.HostManage) {
		return nil
	}
	ok, e := q.Manager(ctx, livesql.ManagerParams{AgencyID: id, AccountID: u.Actor()})
	if e != nil {
		return e
	}
	if !ok {
		return live.Fail(apperror.Forbidden)
	}
	return nil
}
func eligible(ctx context.Context, q *livesql.Queries, tx pgx.Tx, id, kind string, l live.Limits) (livesql.AppAgencyHostAssociation, error) {
	a, e := q.Association(ctx, id)
	if e != nil {
		return a, e
	}
	g, e := q.Agency(ctx, a.AgencyID)
	if e != nil {
		return a, e
	}
	account, e := identitypg.BindSecurity(tx).Account(ctx, id)
	if e != nil {
		return a, e
	}
	if account.Status != "active" || g.Status != "active" || !l.Compatibility.Allows(a.Classification, kind) {
		return a, live.Fail(apperror.Forbidden)
	}
	return a, nil
}
func audit(ctx context.Context, u access.Unit, target, action, reason string, fields ...string) error {
	id := requestmeta.ID(ctx)
	if id == "" {
		id = access.ID()
	}
	return u.Audit(ctx, access.Audit{Actor: u.Actor(), Target: target, Action: action, Fields: append([]string{"live_agency"}, fields...), Reason: reason, Correlation: id})
}
func (r *Repository) Ready(ctx context.Context) error {
	v, e := livesql.New(r.pool).Ready(ctx)
	if e != nil || !v.Valid || !v.Bool {
		return live.Fail(apperror.ServiceUnavailable)
	}
	return nil
}
func (r *Repository) Execute(ctx context.Context, c access.Credentials, in live.Command) (out live.Result, err error) {
	if e := in.Validate(r.limits); e != nil {
		return out, e
	}
	ctx, cancel := context.WithTimeout(ctx, r.limits.DatabaseTimeout)
	defer cancel()
	permission := ""
	target := in.Target
	switch in.Kind {
	case "live.ban":
		permission = "live.participant.global_ban"
	case "policy.publish":
		permission = live.PolicyManage
	case "agency.update":
		permission = live.AgencyManage
	case "host.classify":
		permission = live.HostManage
	}
	if in.Admin {
		permission = live.RoomModerate
	}
	fingerprint, e := live.Fingerprint(in)
	if e != nil {
		return out, safe(e)
	}
	err = r.guard.Authorized(ctx, c, permission, false, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := livesql.New(tx)
		// Recheck the persisted restriction at the mutation boundary, before
		// idempotency replay. A protected owner has no live-ban exemption.
		if in.Kind == "live.ban" || strings.HasPrefix(in.Kind, "room.") || strings.HasPrefix(in.Kind, "seat.") || strings.HasPrefix(in.Kind, "member.") {
			banned, e := q.LiveRestricted(ctx, u.Actor())
			if e != nil {
				return e
			}
			if banned {
				return live.Fail(apperror.Forbidden)
			}
		}
		if in.Kind != "policy.publish" {
			limits, e := r.effective(ctx, q)
			if e != nil {
				return e
			}
			if e := in.Validate(limits); e != nil {
				return e
			}
		}
		prior, e := q.Existing(ctx, livesql.ExistingParams{ActorID: u.Actor(), Kind: in.Kind, Key: in.Key})
		if e == nil {
			if strings.HasPrefix(in.Kind, "room.") && in.Kind != "room.create" || strings.HasPrefix(in.Kind, "seat.") || strings.HasPrefix(in.Kind, "member.") {
				effective, e := accesspg.Resolve(ctx, tx, u.Actor(), in.ID)
				if e != nil {
					return e
				}
				for _, action := range requiredRoomActions(in) {
					if !effective.Allows(action) {
						return live.Fail(apperror.Forbidden)
					}
				}
			}
			if prior.Fingerprint != fingerprint {
				return live.Fail(apperror.Conflict)
			}
			return json.Unmarshal(prior.Result, &out)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if in.Kind == "live.ban" {
			out, e = r.globalBan(ctx, tx, q, u, in)
		} else if len(in.Kind) > 5 && in.Kind[:5] == "room." || len(in.Kind) > 5 && in.Kind[:5] == "seat." || len(in.Kind) > 7 && in.Kind[:7] == "member." {
			out, e = r.roomCommand(ctx, tx, q, u, in)
		} else {
			out, e = r.agencyCommand(ctx, tx, q, u, in)
		}
		if e != nil {
			return e
		}
		auditTarget := in.ID
		if auditTarget == "" {
			auditTarget = in.Target
		}
		if auditTarget == "" {
			auditTarget = out.RoomID
		}
		if auditTarget == "" {
			auditTarget = u.Actor()
		}
		switch v := out.Data.(type) {
		case live.Policy:
			auditTarget = v.ID
		case live.Agency:
			auditTarget = v.ID
		}
		fields := []string{"command:" + fingerprint}
		if in.Kind == "live.ban" {
			if in.Muted {
				fields = append(fields, "global_live_banned:true")
			} else {
				fields = append(fields, "global_live_banned:false")
			}
		}
		if in.Target != "" {
			fields = append(fields, "account:"+in.Target)
		}
		if e := audit(ctx, u, auditTarget, in.Kind, in.Reason, fields...); e != nil {
			return e
		}
		b, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return q.Save(ctx, livesql.SaveParams{ActorID: u.Actor(), Kind: in.Kind, Key: in.Key, Fingerprint: fingerprint, Result: string(b)})
	})
	return out, safe(err)
}

// Sessions returns only completed provider intervals. Open or invalidated gaps
// are never extrapolated; clipping preserves historical association boundaries.
func (r *Repository) Sessions(ctx context.Context, id string, start, end time.Time) ([]agency.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, e := livesql.New(r.pool).Activity(ctx, livesql.ActivityParams{AccountID: id, EndedAt: ts(start), StartedAt: ts(end)})
	if e != nil {
		return nil, e
	}
	if len(rows) > agency.MaxSessions {
		return nil, agency.ErrInvalidActivity
	}
	out := make([]agency.Session, 0, len(rows))
	for _, v := range rows {
		if v.EndedAt.Valid && v.EndedAt.Time.After(v.StartedAt.Time) {
			out = append(out, agency.Session{ID: v.ParticipantSid, AccountID: v.AccountID, Start: v.StartedAt.Time.UTC(), End: v.EndedAt.Time.UTC()})
		}
	}
	return out, nil
}

func (r *Repository) effective(ctx context.Context, q *livesql.Queries) (live.Limits, error) {
	l := r.limits
	raw, e := q.LatestLivePolicy(ctx)
	if errors.Is(e, pgx.ErrNoRows) {
		return l, nil
	}
	if e != nil {
		return l, e
	}
	var p live.Policy
	if e := json.Unmarshal(raw.Config, &p); e != nil {
		return l, e
	}
	if p.ID != raw.ID {
		return l, live.Fail(apperror.ServiceUnavailable)
	}
	if e := (live.Command{Kind: "policy.publish", Key: "internal", Reason: "Validate policy", Policy: &p}).Validate(r.limits); e != nil {
		return l, e
	}
	l.Capacity = p.Capacity
	l.Seats = p.Seats
	l.Compatibility = p.Compatibility
	return l, nil
}
