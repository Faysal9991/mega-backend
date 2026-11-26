package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Read(ctx context.Context, c access.Credentials, in live.Query) (out live.Page, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.limits.DatabaseTimeout)
	defer cancel()
	p := ""
	switch in.Kind {
	case "global.ban":
		p = "live.participant.global_ban"
	case "policy":
		p = live.PolicyManage
	case "admin.rooms", "admin.room":
		p = live.RoomModerate
	case "admin.agencies":
		p = live.AgencyManage
	case "activity":
		p = live.ActivityRead
	case "history":
		p = live.HostManage
	}
	err = r.guard.Authorized(ctx, c, p, false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := livesql.New(tx)
		limit := int32(in.Limit + 1)
		switch in.Kind {
		case "global.ban":
			if _, e := q.AccountActive(ctx, in.ID); e != nil {
				return e
			}
			banned, e := q.LiveRestricted(ctx, in.ID)
			if e != nil {
				return e
			}
			out.Data = map[string]any{"account_id": in.ID, "global_live_banned": banned}

		case "room.history":
			effective, e := accesspg.Resolve(ctx, tx, u.Actor(), in.ID)
			if e != nil {
				return e
			}
			if !effective.Allows("room.moderation_history.read") {
				return live.Fail(apperror.Forbidden)
			}
			rows, e := q.RoomHistory(ctx, livesql.RoomHistoryParams{Target: in.ID, ID: in.Cursor, PageLimit: limit})
			if e != nil {
				return e
			}
			items := []map[string]any{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, map[string]any{"event_id": v.ID, "actor_id": v.Actor, "action": v.Action, "reason": v.Reason, "occurred_at": v.OccurredAt.Time.UTC()})
				out.Next = v.ID
			}
			out.Data = items
		case "policy":
			v, e := q.LatestLivePolicy(ctx)
			if errors.Is(e, pgx.ErrNoRows) {
				out.Data = struct {
					Configured bool `json:"configured"`
				}{false}
				return nil
			}
			if e != nil {
				return e
			}
			var p live.Policy
			if e := json.Unmarshal(v.Config, &p); e != nil {
				return e
			}
			p.CreatedAt = v.CreatedAt.Time.UTC()
			out.Data = p
		case "rooms":
			var e error
			out, e = discovery(ctx, q, u, in)
			return e
		case "admin.rooms":
			rows, e := q.Rooms(ctx, livesql.RoomsParams{ID: in.Cursor, Admin: in.Kind == "admin.rooms", PageLimit: limit})
			if e != nil {
				return e
			}
			items := []live.Room{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, room(v))
				out.Next = v.ID
			}
			out.Data = items
		case "room", "admin.room":
			v, e := q.Room(ctx, in.ID)
			if e != nil {
				return e
			}
			if in.Kind == "room" {
				effective, e := accesspg.Resolve(ctx, tx, u.Actor(), in.ID)
				if e != nil {
					return e
				}
				if !effective.Allows("room.enter") {
					return live.Fail(apperror.Forbidden)
				}
				m, e := q.Member(ctx, livesql.MemberParams{RoomID: in.ID, AccountID: u.Actor()})
				if e != nil {
					return e
				}
				if m.State != "active" {
					return live.Fail(apperror.Forbidden)
				}
			}
			rows, e := q.Members(ctx, in.ID)
			if e != nil {
				return e
			}
			items := []live.Member{}
			for _, m := range rows {
				if in.Kind == "room" && m.Hidden {
					continue
				}
				items = append(items, member(m))
			}
			pending, e := q.Pending(ctx, in.ID)
			if e != nil {
				return e
			}
			out.Data = struct {
				Room            live.Room     `json:"room"`
				Members         []live.Member `json:"members"`
				ProviderPending bool          `json:"provider_pending"`
			}{room(v), items, pending}
			if in.Kind == "admin.room" {
				type operation struct {
					ID         string     `json:"operation_id"`
					Kind       string     `json:"kind"`
					Target     string     `json:"target"`
					State      string     `json:"state"`
					Attempts   int32      `json:"attempts"`
					LeaseUntil *time.Time `json:"lease_until"`
				}
				rows, e := q.OperationPreview(ctx, in.ID)
				if e != nil {
					return e
				}
				ops := []operation{}
				for i, v := range rows {
					if i == 50 {
						break
					}
					ops = append(ops, operation{v.ID, v.Kind, v.Target, v.State, v.Attempts, optional(v.LeaseUntil)})
				}
				out.Data = struct {
					Room            live.Room     `json:"room"`
					Members         []live.Member `json:"members"`
					ProviderPending bool          `json:"provider_pending"`
					Operations      []operation   `json:"pending_operations"`
					More            bool          `json:"operations_has_more"`
				}{room(v), items, pending, ops, len(rows) > 50}
			}

		case "agency":
			v, e := q.Agency(ctx, in.ID)
			if e != nil {
				return e
			}
			out.Data = ag(v)
		case "admin.agencies":
			rows, e := q.Agencies(ctx, livesql.AgenciesParams{ID: in.Cursor, Limit: limit})
			if e != nil {
				return e
			}
			items := []live.Agency{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, ag(v))
				out.Next = v.ID
			}
			out.Data = items
		case "applications":
			if e := manager(ctx, q, u, in.ID); e != nil {
				return e
			}
			rows, e := q.Applications(ctx, livesql.ApplicationsParams{AgencyID: in.ID, ID: in.Cursor, Limit: limit})
			if e != nil {
				return e
			}
			items := []live.Application{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, application(v))
				out.Next = v.ID
			}
			out.Data = items
		case "history", "my.history":
			id := in.ID
			if in.Kind == "my.history" {
				id = u.Actor()
			}
			rows, e := q.History(ctx, livesql.HistoryParams{AccountID: id, ID: in.Cursor, Limit: limit})
			if e != nil {
				return e
			}
			items := []live.Association{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, association(v))
				out.Next = v.ID
			}
			out.Data = items
		case "activity":
			rows, e := q.InspectActivity(ctx, livesql.InspectActivityParams{AccountID: in.ID, ParticipantSid: in.Cursor, Limit: limit})
			if e != nil {
				return e
			}
			type segment struct {
				ID             string     `json:"segment_id"`
				RoomID         string     `json:"room_id"`
				AssociationID  string     `json:"association_id"`
				Classification string     `json:"classification"`
				Start          *time.Time `json:"start"`
				End            *time.Time `json:"end"`
				ValidUntil     *time.Time `json:"valid_until"`
				RestrictedAt   *time.Time `json:"account_restricted_at"`
			}
			items := []segment{}
			for i, v := range rows {
				if i == in.Limit {
					out.More = true
					break
				}
				items = append(items, segment{v.ParticipantSid, v.RoomID, v.AssociationID, v.Classification, optional(v.StartedAt), optional(v.EndedAt), optional(v.ValidUntil), optional(v.RestrictedAt)})
				out.Next = v.ParticipantSid
			}
			out.Data = items

		default:
			return live.Fail(apperror.ValidationFailed)
		}
		return nil
	})
	return out, safe(err)
}
