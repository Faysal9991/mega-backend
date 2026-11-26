package restrictions

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/jackc/pgx/v5"
)

// CutParticipation applies an entry denial without changing account/device state.
// The caller owns the same security transaction and its mandatory audit record.
func CutParticipation(ctx context.Context, tx pgx.Tx, account, scope string) error {
	q := livesql.New(tx)
	rooms, e := q.ActiveRooms(ctx, account)
	if e != nil {
		return e
	}
	if len(rooms) > live.MaxParticipatingRooms {
		return errors.New("live: participation bound exceeded")
	}
	selected := []string{}
	ops := []livesql.EnqueueMembersParams{}
	for _, id := range rooms {
		if scope != "" && scope != id {
			continue
		}
		selected = append(selected, id)
		ops = append(ops, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: id, Kind: "participant", Target: account})
	}
	if len(selected) == 0 {
		return nil
	}
	if e = q.InvalidateScopedParticipation(ctx, livesql.InvalidateScopedParticipationParams{AccountID: account, Column2: selected}); e != nil {
		return e
	}
	if e = q.LeaveRestrictedRooms(ctx, livesql.LeaveRestrictedRoomsParams{AccountID: account, RoomIds: selected}); e != nil {
		return e
	}
	_, e = q.EnqueueMembers(ctx, ops)
	return e
}
