package postgres

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/jackc/pgx/v5"
)

// RecoverCapabilities scans a bounded indexed page and commits corrections before
// remote calls. The cursor is only scheduling progress, never authority.
func (r *Repository) RecoverCapabilities(ctx context.Context, cursorRoom, cursorAccount string, p live.Provider) (nextRoom, nextAccount string, err error) {
	err = r.tx.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if e := accesssql.New(tx).LockSecurity(ctx); e != nil {
			return e
		}
		q := livesql.New(tx)
		rows, e := q.CapabilityMembers(ctx, livesql.CapabilityMembersParams{RoomID: cursorRoom, AccountID: cursorAccount})
		if e != nil {
			return e
		}
		for _, v := range rows {
			nextRoom, nextAccount = v.RoomID, v.AccountID
			effective, e := accesspg.Resolve(ctx, tx, v.AccountID, v.RoomID)
			if e != nil {
				return e
			}
			hidden, self, private := v.Hidden, v.SelfSeated, v.PrivateEntry
			leave := !effective.Allows("room.enter")
			if hidden && !effective.Allows("room.join.hidden") {
				hidden = false
			}
			if self && !effective.Allows("room.seat.self_join") {
				self = false
			}
			if private {
				room, e := q.Room(ctx, v.RoomID)
				if e != nil {
					return e
				}
				if room.Visibility == "public" {
					private = false
				}
			}
			if hidden == v.Hidden && self == v.SelfSeated && private == v.PrivateEntry && !leave {
				continue
			}
			m, e := q.Member(ctx, livesql.MemberParams{RoomID: v.RoomID, AccountID: v.AccountID})
			if e != nil {
				return e
			}
			state := m.State
			if v.Hidden && !hidden || leave {
				state = "left"
				hidden = false
				self = false
				private = false
			}
			seat := m.Seat.Int32
			if !self && v.SelfSeated {
				seat = 0
			}
			if _, e = q.UpdateMember(ctx, livesql.UpdateMemberParams{RoomID: v.RoomID, AccountID: v.AccountID, Role: m.Role, State: state, Muted: m.Muted, Seat: seat, InvitedSeat: m.InvitedSeat.Int32}); e != nil {
				return e
			}
			if e = q.MemberFlags(ctx, livesql.MemberFlagsParams{RoomID: v.RoomID, AccountID: v.AccountID, Hidden: hidden, SelfSeated: self, PrivateEntry: private}); e != nil {
				return e
			}
			if e = q.InvalidatePresence(ctx, livesql.InvalidatePresenceParams{RoomID: v.RoomID, AccountID: v.AccountID}); e != nil {
				return e
			}
			if e = q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: v.RoomID, Kind: "participant", Target: v.AccountID}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return "", "", safe(err)
	}
	if p == nil {
		return nextRoom, nextAccount, nil
	}
	ids, e := livesql.New(r.pool).PendingRoomIDs(ctx)
	if e != nil {
		return nextRoom, nextAccount, safe(e)
	}
	for _, id := range ids {
		if e = r.Flush(ctx, id, p); e != nil {
			err = e
			if ctx.Err() != nil {
				return nextRoom, nextAccount, e
			}
		}
	}

	return
}
