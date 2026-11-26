package postgres

import (
	"context"
	"errors"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/intents"
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
		if !u.Effective().Allows("room.enter") {
			return out, live.Fail(apperror.Forbidden)
		}
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

	effective, e := accesspg.Resolve(ctx, tx, actor, in.ID)
	if e != nil {
		return out, e
	}
	action := roomAction(in)
	for _, required := range requiredRoomActions(in) {
		if !effective.Allows(required) {
			return out, live.Fail(apperror.Forbidden)
		}
	}
	own := actor == v.OwnerID
	admin := has(u, live.RoomModerate) || action != "" && u.Effective().Allows(action)
	am, ae := q.Member(ctx, livesql.MemberParams{RoomID: in.ID, AccountID: actor})
	if ae != nil && !errors.Is(ae, pgx.ErrNoRows) {
		return out, ae
	}
	moderate := action != "" && effective.Allows(action)
	if in.Kind == "room.close" {
		if !effective.Allows("room.close") {
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
	if in.Kind == "room.settings" || in.Kind == "seat.lock" || in.Kind == "seat.unlock" {
		title, announcement, visibility, locked := v.Title, v.Announcement, v.Visibility, v.SeatsLocked
		if in.Kind == "room.settings" {
			if in.TitleSet {
				if !effective.Allows("room.title.update") {
					return out, live.Fail(apperror.Forbidden)
				}
				title = in.Name
			}
			if in.AnnouncementSet {
				if !effective.Allows("room.announcement.update") {
					return out, live.Fail(apperror.Forbidden)
				}
				announcement = in.Announcement
			}
			if in.VisibilitySet {
				if !effective.Allows("room.settings.update") {
					return out, live.Fail(apperror.Forbidden)
				}
				visibility = in.Visibility
			}
		} else {
			locked = in.Kind == "seat.lock"
		}
		if e = q.RoomSettings(ctx, livesql.RoomSettingsParams{ID: in.ID, Title: title, Announcement: announcement, Visibility: visibility, SeatsLocked: locked}); e != nil {
			return out, e
		}
		out.Data = map[string]any{"room_id": in.ID, "title": title, "announcement": announcement, "visibility": visibility, "seats_locked": locked}
		return out, nil
	}
	if in.Kind == "room.join" {
		if v.Visibility != "public" && !own && !(ae == nil && am.State == "active") && !effective.Allows("room.enter.private") {
			return out, live.Fail(apperror.Forbidden)
		}
		if !effective.Allows("room.enter") {
			return out, live.Fail(apperror.Forbidden)
		}
		if in.Hidden && !effective.Allows("room.join.hidden") {
			return out, live.Fail(apperror.Forbidden)
		}
		if ae == nil && am.State == "active" {
			if in.Hidden && (am.Seat.Valid || am.Role == "host" || am.Role == "co_host") {
				return out, live.Fail(apperror.Conflict)
			}
			if e = q.MemberFlags(ctx, livesql.MemberFlagsParams{RoomID: in.ID, AccountID: actor, Hidden: in.Hidden, SelfSeated: am.SelfSeated, PrivateEntry: am.PrivateEntry && v.Visibility != "public"}); e != nil {
				return out, e
			}
			am.Hidden = in.Hidden
			am.PrivateEntry = am.PrivateEntry && v.Visibility != "public"
			out.Data = member(am)
			out.RoomID = in.ID
			out.OperationID = access.ID()
			return out, q.Enqueue(ctx, livesql.EnqueueParams{ID: out.OperationID, RoomID: in.ID, Kind: "participant", Target: actor})
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
			if in.Hidden {
				return out, live.Fail(apperror.Conflict)
			}
			if _, e := eligible(ctx, q, tx, actor, v.Kind, limits); e != nil {
				return out, e
			}
		}
		m, e := q.JoinMember(ctx, livesql.JoinMemberParams{RoomID: in.ID, AccountID: actor, Role: role})
		if e != nil {
			return out, e
		}
		if e = q.MemberFlags(ctx, livesql.MemberFlagsParams{RoomID: in.ID, AccountID: actor, Hidden: in.Hidden, PrivateEntry: v.Visibility != "public" && !own}); e != nil {
			return out, e
		}
		m.Hidden = in.Hidden
		m.PrivateEntry = v.Visibility != "public" && !own
		out.Data = member(m)
		return out, nil
	}
	if moderate && !admin && (own || am.Role == "co_host") && in.Kind != "room.leave" && in.Kind != "seat.release" {
		if _, e := eligible(ctx, q, tx, actor, v.Kind, limits); e != nil {
			return out, e
		}
	}
	target := in.Target
	switch in.Kind {
	case "room.leave", "seat.accept", "seat.release", "seat.self_join":
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
	if target == v.OwnerID && !(admin && target != actor && (in.Kind == "member.kick" || in.Kind == "member.mute" || in.Kind == "seat.remove" || in.Kind == "member.ban" || in.Kind == "member.unban")) {
		return out, live.Fail(apperror.Forbidden)
	}
	if m.State != "active" && in.Kind != "member.unban" {
		return out, live.Fail(apperror.Conflict)
	}
	next := live.Member{AccountID: target, Role: m.Role, State: m.State, Muted: m.Muted, Seat: m.Seat.Int32, InvitedSeat: m.InvitedSeat.Int32, Hidden: m.Hidden, SelfSeated: m.SelfSeated, PrivateEntry: m.PrivateEntry}
	switch in.Kind {
	case "room.leave":
		next.State = "left"
		next.Seat = 0
		next.InvitedSeat = 0
	case "seat.self_join":
		if v.SeatsLocked || next.Hidden {
			return out, live.Fail(apperror.Conflict)
		}
		members, e := q.Members(ctx, in.ID)
		if e != nil {
			return out, e
		}
		available := v.Seats
		if available > int32(limits.Seats) {
			available = int32(limits.Seats)
		}
		if v.Kind == "video" {
			available = int32(limits.Seats)
			if available > v.Capacity {
				available = v.Capacity
			}
			publishers := 0
			for _, m := range members {
				if m.Role == "host" || m.Role == "co_host" || m.SelfSeated && m.Seat.Valid {
					publishers++
				}
			}
			if publishers >= int(available) {
				return out, live.Fail(apperror.Conflict)
			}
		}
		occupied := map[int32]bool{}
		for _, member := range members {
			if member.Seat.Valid {
				occupied[member.Seat.Int32] = true
			}
		}
		seat := in.Seat
		if seat == 0 {
			for i := int32(1); i <= available; i++ {
				if !occupied[i] {
					seat = i
					break
				}
			}
		}
		if seat < 1 || seat > available || occupied[seat] {
			return out, live.Fail(apperror.Conflict)
		}
		next.Seat = seat
		next.InvitedSeat = 0
		next.SelfSeated = true
	case "seat.invite":
		if v.SeatsLocked {
			return out, live.Fail(apperror.Conflict)
		}
		if v.Kind != "audio" || in.Seat > v.Seats {
			return out, live.Fail(apperror.ValidationFailed)
		}
		if _, e := eligible(ctx, q, tx, target, v.Kind, limits); e != nil {
			return out, e
		}
		next.InvitedSeat = in.Seat
	case "seat.accept":
		if v.SeatsLocked || next.Hidden {
			return out, live.Fail(apperror.Conflict)
		}
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
		if !effective.Allows(roomAction(in)) {
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
	if in.Kind == "seat.invite" {
		if e := intents.Notify(ctx, tx, target, "room_invitation", in.Key); e != nil {
			return out, e
		}
	}
	if next.Seat == 0 {
		next.SelfSeated = false
	}
	if e = q.MemberFlags(ctx, livesql.MemberFlagsParams{RoomID: in.ID, AccountID: target, Hidden: next.Hidden, SelfSeated: next.SelfSeated, PrivateEntry: next.PrivateEntry}); e != nil {
		return out, e
	}
	updated.Hidden = next.Hidden
	updated.SelfSeated = next.SelfSeated
	updated.PrivateEntry = next.PrivateEntry
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
		if banned, e := q.LiveRestricted(ctx, u.Actor()); e != nil {
			return e
		} else if banned {
			return live.Fail(apperror.Forbidden)
		}
		effective, e := accesspg.Resolve(ctx, tx, u.Actor(), id)
		if e != nil {
			return e
		}
		if !effective.Allows("room.enter") || m.Hidden && !effective.Allows("room.join.hidden") || m.SelfSeated && !effective.Allows("room.seat.self_join") {
			return live.Fail(apperror.Forbidden)
		}
		if m.Hidden && (m.Seat.Valid || m.Role == "host" || m.Role == "co_host") {
			return live.Fail(apperror.Conflict)
		}
		if m.Role == "host" || m.Role == "co_host" || m.Seat.Valid && !m.SelfSeated {
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
	if banned, e := q.LiveRestricted(ctx, account); e != nil {
		return e
	} else if banned {
		return live.Fail(apperror.Forbidden)
	}
	rooms, e := q.ActiveRooms(ctx, account)
	if e != nil {
		return e
	}
	if len(rooms) >= live.MaxParticipatingRooms {
		return live.Fail(apperror.Conflict)
	}
	return nil
}

func roomAction(in live.Command) string {
	switch in.Kind {
	case "room.join":
		return "room.enter"
	case "room.close":
		return "room.close"
	case "room.settings":
		return ""
	case "seat.lock":
		return "room.seat.lock"
	case "seat.unlock":
		return "room.seat.unlock"
	case "seat.self_join":
		return "room.seat.self_join"
	case "seat.release":
		return "room.seat.self_leave"
	case "seat.remove":
		return "room.participant.seat_remove"
	case "seat.invite":
		return "room.participant.seat_remove"
	case "member.kick":
		return "room.participant.kick"
	case "member.mute":
		return "room.participant.mute"
	case "member.ban", "member.unban":
		return "room.participant.ban"
	case "member.role":
		if in.Role == "moderator" {
			return "room.moderator.assign"
		}
		return "room.admin.assign"
	}
	return ""
}
func (r *Repository) globalBan(ctx context.Context, tx pgx.Tx, q *livesql.Queries, u access.Unit, in live.Command) (out live.Result, err error) {
	if !u.Effective().Allows("live.participant.global_ban") {
		return out, live.Fail(apperror.Forbidden)
	}
	if e := protect(ctx, tx, in.Target, false); e != nil {
		return out, e
	}
	if e := q.SetLiveBan(ctx, livesql.SetLiveBanParams{AccountID: in.Target, Active: in.Muted}); e != nil {
		return out, e
	}
	if in.Muted {
		rooms, e := q.ActiveRooms(ctx, in.Target)
		if e != nil {
			return out, e
		}
		if len(rooms) > live.MaxParticipatingRooms {
			return out, live.Fail(apperror.ServiceUnavailable)
		}
		if e = q.InvalidateMembers(ctx, []string{in.Target}); e != nil {
			return out, e
		}
		ops := make([]livesql.EnqueueMembersParams, 0, len(rooms))
		for _, id := range rooms {
			ops = append(ops, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: id, Kind: "participant", Target: in.Target})
		}
		if len(ops) > 0 {
			if _, e = q.EnqueueMembers(ctx, ops); e != nil {
				return out, e
			}
		}
		if e = q.LeaveRestrictedRooms(ctx, livesql.LeaveRestrictedRoomsParams{AccountID: in.Target, RoomIds: rooms}); e != nil {
			return out, e
		}
	}
	if in.Muted {
		out.ProviderState = "pending"
	}
	out.Data = map[string]any{"account_id": in.Target, "global_live_banned": in.Muted}
	return out, nil
}

func requiredRoomActions(in live.Command) []string {
	out := []string{}
	if action := roomAction(in); action != "" {
		out = append(out, action)
	}
	if in.Kind == "room.join" && in.Hidden {
		out = append(out, "room.join.hidden")
	}
	if in.Kind == "seat.accept" {
		out = append(out, "room.enter")
	}
	if in.Kind == "room.settings" {
		if in.TitleSet {
			out = append(out, "room.title.update")
		}
		if in.AnnouncementSet {
			out = append(out, "room.announcement.update")
		}
		if in.VisibilitySet {
			out = append(out, "room.settings.update")
		}
	}
	return out
}
