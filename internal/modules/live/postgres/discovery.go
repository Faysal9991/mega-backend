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

func discovery(ctx context.Context, q *livesql.Queries, u access.Unit, in live.Query) (out live.Page, err error) {
	if !u.Effective().Allows("room.enter") {
		return out, live.Fail(apperror.Forbidden)
	}
	if in.Cursor == "" {
		if e := q.PruneListSnapshots(ctx); e != nil {
			return out, e
		}
	}
	state, e := q.DiscoveryClock(ctx, u.Actor())
	if e != nil {
		return out, e
	}
	cursor := live.ListCursor{}
	validUntil := state.ExpiresAt
	if in.Cursor != "" {
		var ok bool
		cursor, ok = live.ParseListCursor(in.Cursor)
		if !ok {
			return out, live.Fail(apperror.ValidationFailed)
		}
		v, e := q.ListSnapshot(ctx, livesql.ListSnapshotParams{ID: cursor.Snapshot, ActorID: u.Actor()})
		if errors.Is(e, pgx.ErrNoRows) {
			return out, live.Fail(apperror.LiveListRestartRequired)
		}
		if e != nil {
			return out, e
		}
		validUntil = v.ExpiresAt
		if v.Revision != state.Revision {
			return out, live.Fail(apperror.LiveListRestartRequired)
		}
	}
	rows, e := q.Discovery(ctx, livesql.DiscoveryParams{Actor: u.Actor(), AsOf: state.Now, FirstPage: in.Cursor == "", Priority: cursor.Priority, Cursor: cursor.Room, PageLimit: int32(in.Limit + 1)})
	if e != nil {
		return out, e
	}
	items := []live.RankedRoom{}
	for i, v := range rows {
		if i == in.Limit {
			out.More = true
			break
		}
		items = append(items, live.RankedRoom{Room: live.Room{ID: v.ID, SessionID: v.SessionID, OwnerID: v.OwnerID, Kind: v.Kind, Title: v.Title, State: v.State, Capacity: v.Capacity, Seats: v.Seats, CreatedAt: v.CreatedAt.Time, ClosedAt: optional(v.ClosedAt), Visibility: v.Visibility, Announcement: v.Announcement, SeatsLocked: v.SeatsLocked}, Priority: v.ListingPriority})
	}
	if out.More {
		if cursor.Snapshot == "" {
			n, e := q.ActorListSnapshots(ctx, u.Actor())
			if e != nil {
				return out, e
			}
			if n >= 5 {
				return out, live.Fail(apperror.RateLimited)
			}
			cursor.Snapshot = access.ID()
			if e = q.CreateListSnapshot(ctx, livesql.CreateListSnapshotParams{ID: cursor.Snapshot, ActorID: u.Actor(), Revision: state.Revision, ExpiresAt: state.ExpiresAt}); e != nil {
				return out, e
			}
		}
		last := items[len(items)-1]
		out.Next = (live.ListCursor{Snapshot: cursor.Snapshot, Room: last.ID, Priority: last.Priority}).String()
	}
	valid, e := q.DiscoveryStillValid(ctx, livesql.DiscoveryStillValidParams{Revision: state.Revision, Column2: validUntil})
	if e != nil {
		return out, e
	}
	if !valid.Valid || !valid.Bool {
		return out, live.Fail(apperror.LiveListRestartRequired)
	}
	out.Data = items
	return out, nil
}
