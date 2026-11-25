package postgres

import (
	"context"
	"errors"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) roomCommand(ctx context.Context, tx pgx.Tx, q *livesql.Queries, u access.Unit, in live.Command) (out live.Result, err error) {
	limits, e := r.effective(ctx, q)
	if e != nil {
		return out, e
	}
	actor := u.Actor()
	if in.Kind == "room.create" {
		if e := canEnter(ctx, q, actor); e != nil {
			return out, e
		}
		if _, e := eligible(ctx, q, tx, actor, in.Status, limits); e != nil {
			return out, e
		}
		v, e := q.CreateRoom(ctx, livesql.CreateRoomParams{ID: access.ID(), SessionID: access.ID(), OwnerID: actor, Kind: in.Status, Title: in.Name, Capacity: in.Capacity, Seats: in.Seats})
		if e != nil {
			return out, e
		}
		if _, e = q.JoinMember(ctx, livesql.JoinMemberParams{RoomID: v.ID, AccountID: actor, Role: "host"}); e != nil {
			return out, e
		}
		out.Data = room(v)
		out.RoomID = v.ID
		out.OperationID = access.ID()
		return out, q.Enqueue(ctx, livesql.EnqueueParams{ID: out.OperationID, RoomID: v.ID, Kind: "create"})
	}
	v, e := q.Room(ctx, in.ID)
	if e != nil {
		return out, e
	}
	own := actor == v.OwnerID
	admin := has(u, live.RoomModerate)
	am, ae := q.Member(ctx, livesql.MemberParams{RoomID: in.ID, AccountID: actor})
	if ae != nil && !errors.Is(ae, pgx.ErrNoRows) {
		return out, ae
	}
	moderate := own || admin || ae == nil && am.State == "active" && (am.Role == "moderator" || am.Role == "co_host")
	if in.Kind == "room.close" {
		if !own && !admin {
			return out, live.Fail(apperror.Forbidden)
		}
		if v.State == "closed" || v.State == "closing" {
			out.Data = room(v)
			return out, nil
		}
		if e := q.RoomState(ctx, livesql.RoomStateParams{ID: in.ID, State: "closing"}); e != nil {
			return out, e
		}
		if e := q.CloseMembers(ctx, in.ID); e != nil {
			return out, e
		}
		if e := q.InvalidateRoomPresence(ctx, in.ID); e != nil {
			return out, e
		}
		out.RoomID = in.ID
		out.OperationID = access.ID()
		out.Data = struct {
			State string `json:"state"`
		}{"closing"}
		return out, q.Enqueue(ctx, livesql.EnqueueParams{ID: out.OperationID, RoomID: in.ID, Kind: "close"})
	}
	if v.State != "open" {
		return out, live.Fail(apperror.Conflict)
	}
	if in.Kind == "room.join" {
		if ae == nil && am.State == "active" {
			out.Data = member(am)
			return out, nil
		}
		if ae == nil && (am.State == "banned" || am.State == "kicked") {
			return out, live.Fail(apperror.Forbidden)
		}
		if e := canEnter(ctx, q, actor); e != nil {
			return out, e
		}
		members, e := q.Members(ctx, in.ID)
		if e != nil {
			return out, e
		}
		if len(members) >= int(v.Capacity) {
			return out, live.Fail(apperror.Conflict)
		}
		role := "audience"
		if own {
			role = "host"
			if _, e := eligible(ctx, q, tx, actor, v.Kind, limits); e != nil {
				return out, e
			}
		}
		m, e := q.JoinMember(ctx, livesql.JoinMemberParams{RoomID: in.ID, AccountID: actor, Role: role})
		out.Data = member(m)
		return out, e
	}
	if moderate && !admin && (own || am.Role == "co_host") && in.Kind != "room.leave" && in.Kind != "seat.release" {
		if _, e := eligible(ctx, q, tx, actor, v.Kind, limits); e != nil {
			return out, e
		}
	}
	target := in.Target
	switch in.Kind {
	case "room.leave", "seat.accept", "seat.release":
		target = actor
	default:
		if !moderate {
			return out, live.Fail(apperror.Forbidden)
		}
	}
	if !access.ValidID(target) {
		return out, live.Fail(apperror.ValidationFailed)
	}
	m, e := q.Member(ctx, livesql.MemberParams{RoomID: in.ID, AccountID: target})
	if e != nil {
		return out, e
	}
	if target != actor {
		if e := protect(ctx, tx, target, in.Kind == "seat.invite" || in.Kind == "member.role" && in.Role != "audience"); e != nil {
			return out, e
		}
	}
	if !own && !admin && target != actor && (m.Role == "co_host" || m.Role == "moderator") {
		return out, live.Fail(apperror.Forbidden)
	}
	if target == v.OwnerID {
		return out, live.Fail(apperror.Forbidden)
	}
	if m.State != "active" && in.Kind != "member.unban" {
		return out, live.Fail(apperror.Conflict)
	}
	next := live.Member{AccountID: target, Role: m.Role, State: m.State, Muted: m.Muted, Seat: m.Seat.Int32, InvitedSeat: m.InvitedSeat.Int32}
	switch in.Kind {
	case "room.leave":
		next.State = "left"
		next.Seat = 0
		next.InvitedSeat = 0
	case "seat.invite":
		if v.Kind != "audio" || in.Seat > v.Seats {
			return out, live.Fail(apperror.ValidationFailed)
		}
		if _, e := eligible(ctx, q, tx, target, v.Kind, limits); e != nil {
			return out, e
		}
		next.InvitedSeat = in.Seat
	case "seat.accept":
		if v.Kind != "audio" || next.InvitedSeat == 0 {
			return out, live.Fail(apperror.Conflict)
		}
		if _, e := eligible(ctx, q, tx, target, v.Kind, limits); e != nil {
			return out, e
		}
		next.Seat = next.InvitedSeat
		next.InvitedSeat = 0
	case "seat.release", "seat.remove":
		next.Seat = 0
		next.InvitedSeat = 0
	case "member.role":
		if !own && !admin {
			return out, live.Fail(apperror.Forbidden)
		}
		if target == actor {
			return out, live.Fail(apperror.Forbidden)
		}
		if in.Role == "co_host" {
			if _, e := eligible(ctx, q, tx, target, v.Kind, limits); e != nil {
				return out, e
			}
		}
		next.Role = in.Role
		next.Seat = 0
		next.InvitedSeat = 0
	case "member.mute":
		next.Muted = in.Muted
	case "member.kick":
		next.State = "kicked"
		next.Seat = 0
		next.InvitedSeat = 0
	case "member.ban":
		next.State = "banned"
		next.Seat = 0
		next.InvitedSeat = 0
	case "member.unban":
		if !own && !admin {
			return out, live.Fail(apperror.Forbidden)
		}
		if m.State != "banned" && m.State != "kicked" {
			return out, live.Fail(apperror.Conflict)
		}
		next.State = "left"
	default:
		return out, live.Fail(apperror.ValidationFailed)
	}
	updated, e := q.UpdateMember(ctx, livesql.UpdateMemberParams{RoomID: in.ID, AccountID: target, Role: next.Role, State: next.State, Muted: next.Muted, Seat: next.Seat, InvitedSeat: next.InvitedSeat})
	if e != nil {
		return out, e
	}
	if !live.CanPublish(room(v), next) {
		if e := q.InvalidatePresence(ctx, livesql.InvalidatePresenceParams{RoomID: in.ID, AccountID: target}); e != nil {
			return out, e
		}
	}
	out.Data = member(updated)
	out.RoomID = in.ID
	out.OperationID = access.ID()
	return out, q.Enqueue(ctx, livesql.EnqueueParams{ID: out.OperationID, RoomID: in.ID, Kind: "participant", Target: target})
}
func (r *Repository) Grant(ctx context.Context, c access.Credentials, id string) (out live.Grant, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.limits.DatabaseTimeout)
	defer cancel()
	err = r.guard.Authorized(ctx, c, "", false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := livesql.New(tx)
		limits, e := r.effective(ctx, q)
		if e != nil {
			return e
		}
		v, e := q.Room(ctx, id)
		if e != nil {
			return e
		}
		if v.State != "open" {
			return live.Fail(apperror.Conflict)
		}

		m, e := q.Member(ctx, livesql.MemberParams{RoomID: id, AccountID: u.Actor()})
		if e != nil {
			return e
		}
		if m.State != "active" {
			return live.Fail(apperror.Forbidden)
		}
		pending, e := q.Pending(ctx, id)
		if e != nil {
			return e
		}
		if pending {
			return live.Fail(apperror.ServiceUnavailable)
		}
		if m.Role == "host" || m.Role == "co_host" || m.Seat.Valid {
			if _, e := eligible(ctx, q, tx, u.Actor(), v.Kind, limits); e != nil {
				return e
			}
		}
		out = live.Grant{Room: room(v), Member: member(m), Identity: u.Actor()}
		return nil
	})
	return out, safe(err)
}

func canEnter(ctx context.Context, q *livesql.Queries, account string) error {
	rooms, e := q.ActiveRooms(ctx, account)
	if e != nil {
		return e
	}
	if len(rooms) >= live.MaxParticipatingRooms {
		return live.Fail(apperror.Conflict)
	}
	return nil
}
