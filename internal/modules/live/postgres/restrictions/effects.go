// Package restrictions supplies transaction-bound live effects to Identity-owned
// account restrictions. No provider call or independent transaction occurs here.
package restrictions

import (
	"context"
	"errors"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/jackc/pgx/v5"
)

// Apply persists an immutable accounting ceiling and pending removal intent.
// Completed activity preceding the restriction is never rewritten. The indexed
// source query clips an interval at its first restriction, even after unrestrict.
func Apply(ctx context.Context, tx pgx.Tx, account string) error {
	q := livesql.New(tx)
	rooms, e := q.ActiveRooms(ctx, account)
	if e != nil {
		return e
	}
	if len(rooms) > live.MaxParticipatingRooms {
		return errors.New("live: unsupported participation count")
	}
	if e := q.RecordRestriction(ctx, livesql.RecordRestrictionParams{ID: access.ID(), AccountID: account}); e != nil {
		return e
	}
	ids := []string{}
	ops := []livesql.EnqueueMembersParams{}
	for _, r := range rooms {
		ids = append(ids, r)
		ops = append(ops, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: r, Kind: "participant", Target: account})
	}
	if _, e := q.EnqueueMembers(ctx, ops); e != nil {
		return e
	}
	return q.LeaveRestrictedRooms(ctx, livesql.LeaveRestrictedRoomsParams{AccountID: account, RoomIds: ids})
}
