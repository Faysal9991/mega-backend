package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	agencypg "github.com/Faysal9991/mega-backend/internal/modules/agency/postgres"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) agencyCommand(ctx context.Context, tx pgx.Tx, q *livesql.Queries, u access.Unit, in live.Command) (out live.Result, err error) {
	switch in.Kind {
	case "policy.publish":
		latest, e := q.LatestLivePolicy(ctx)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if e == nil && in.Policy.PreviousID != latest.ID || errors.Is(e, pgx.ErrNoRows) && in.Policy.PreviousID != "" {
			return out, live.Fail(apperror.Conflict)
		}
		rows, e := q.AllAffectedMembers(ctx)
		if e != nil {
			return out, e
		}
		if len(rows) > 500 {
			return out, live.Fail(apperror.Conflict)
		}
		accounts := []string{}
		ops := []livesql.EnqueueMembersParams{}
		for _, v := range rows {
			accounts = append(accounts, v.AccountID)
			ops = append(ops, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: v.RoomID, Kind: "participant", Target: v.AccountID})
		}
		if e := q.InvalidateMembers(ctx, accounts); e != nil {
			return out, e
		}
		if _, e := q.EnqueueMembers(ctx, ops); e != nil {
			return out, e
		}
		p := *in.Policy
		p.ID = access.ID()
		p.CreatedAt = time.Time{}
		b, e := json.Marshal(p)
		if e != nil {
			return out, e
		}
		created, e := q.PublishLivePolicy(ctx, livesql.PublishLivePolicyParams{ID: p.ID, PreviousID: p.PreviousID, ActorID: u.Actor(), Config: string(b)})
		p.CreatedAt = created.Time.UTC()
		out.Data = p
		return out, e

	case "agency.create":
		v, e := q.CreateAgency(ctx, livesql.CreateAgencyParams{ID: u.Actor(), Name: in.Name})
		out.Data = ag(v)
		return out, e
	case "agency.update":
		v, e := q.UpdateAgency(ctx, livesql.UpdateAgencyParams{ID: in.ID, Name: in.Name, Status: in.Status})
		out.Data = ag(v)
		if e != nil {
			return out, e
		}
		if in.Status != "active" {
			return out, r.invalidateMembers(ctx, q, "", in.ID)
		}
		return out, nil
	case "manager.add", "manager.remove":
		if in.Kind == "manager.add" && in.ID == in.Target {
			return out, live.Fail(apperror.ValidationFailed)
		}
		if in.Kind == "manager.add" && in.Target == u.Actor() && in.ID != u.Actor() {
			return out, live.Fail(apperror.Forbidden)
		}
		if in.ID != u.Actor() && !has(u, live.AgencyManage) {
			return out, live.Fail(apperror.Forbidden)
		}
		if e := protect(ctx, tx, in.Target, in.Kind == "manager.add" || in.Kind == "host.classify"); e != nil {
			return out, e
		}
		g, e := q.Agency(ctx, in.ID)
		if e != nil {
			return out, e
		}
		if g.Status != "active" {
			return out, live.Fail(apperror.Forbidden)
		}
		if in.Kind == "manager.add" {
			e = q.AddManager(ctx, livesql.AddManagerParams{AgencyID: in.ID, AccountID: in.Target})
		} else {
			e = q.RemoveManager(ctx, livesql.RemoveManagerParams{AgencyID: in.ID, AccountID: in.Target})
		}
		out.Data = struct {
			AccountID string `json:"account_id"`
		}{in.Target}
		return out, e
	case "host.apply":
		if in.ID == u.Actor() {
			return out, live.Fail(apperror.Forbidden)
		}
		g, e := q.Agency(ctx, in.ID)
		if e != nil {
			return out, e
		}
		if g.Status != "active" {
			return out, live.Fail(apperror.Forbidden)
		}
		_, e = q.Association(ctx, u.Actor())
		if e == nil {
			return out, live.Fail(apperror.Conflict)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		v, e := q.Apply(ctx, livesql.ApplyParams{ID: access.ID(), AgencyID: in.ID, AccountID: u.Actor(), Classification: in.Classification})
		out.Data = application(v)
		return out, e
	case "host.decide":
		v, e := q.Application(ctx, in.ID)
		if e != nil {
			return out, e
		}
		if v.AccountID == u.Actor() {
			return out, live.Fail(apperror.Forbidden)
		}
		if !has(u, live.HostManage) {
			if e := manager(ctx, q, u, v.AgencyID); e != nil {
				return out, e
			}
		}

		if v.State != "pending" {
			return out, live.Fail(apperror.Conflict)
		}
		if in.Status == "approved" {
			_, e := q.Association(ctx, v.AccountID)
			if e == nil {
				return out, live.Fail(apperror.Conflict)
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return out, e
			}
		}
		if e := protect(ctx, tx, v.AccountID, in.Status == "approved"); e != nil {
			return out, e
		}
		g, e := q.Agency(ctx, v.AgencyID)
		if e != nil {
			return out, e
		}
		if g.Status != "active" {
			return out, live.Fail(apperror.Forbidden)
		}
		if in.Status == "approved" {
			if e := agencypg.Bind(tx).InsertNow(ctx, access.ID(), v.AccountID, v.AgencyID, v.Classification); e != nil {
				return out, e
			}
		}
		decided, e := q.Decide(ctx, livesql.DecideParams{ID: in.ID, State: in.Status})
		out.Data = application(decided)
		return out, e
	case "host.leave":
		a, e := q.Association(ctx, u.Actor())
		if e != nil {
			return out, e
		}
		if e := r.invalidateMembers(ctx, q, u.Actor(), ""); e != nil {
			return out, e
		}
		if e := agencypg.Bind(tx).LeaveNow(ctx, u.Actor()); e != nil {
			return out, e
		}
		closed, e := q.HistoricalAssociation(ctx, a.ID)
		out.Data = association(closed)
		return out, e
	case "host.classify":
		if in.Target == u.Actor() {
			return out, live.Fail(apperror.Forbidden)
		}
		if e := protect(ctx, tx, in.Target, in.Kind == "manager.add" || in.Kind == "host.classify"); e != nil {
			return out, e
		}
		a, e := q.Association(ctx, in.Target)
		if e != nil {
			return out, e
		}
		g, e := q.Agency(ctx, a.AgencyID)
		if e != nil {
			return out, e
		}
		if g.Status != "active" {
			return out, live.Fail(apperror.Forbidden)
		}
		if a.Classification == in.Classification {
			out.Data = association(a)
			return out, nil
		}
		if e := r.invalidateMembers(ctx, q, in.Target, ""); e != nil {
			return out, e
		}
		if e := agencypg.Bind(tx).CloseNow(ctx, in.Target); e != nil {
			return out, e
		}
		e = agencypg.Bind(tx).InsertNow(ctx, access.ID(), in.Target, a.AgencyID, in.Classification)
		out.Data = struct {
			Classification string `json:"classification"`
		}{in.Classification}
		return out, e
	}
	return out, live.Fail(apperror.ValidationFailed)
}

// Staff and agency managers cannot mutate protected owners through another domain.
func protect(ctx context.Context, tx pgx.Tx, id string, requireActive bool) error {
	if !access.ValidID(id) {
		return live.Fail(apperror.ValidationFailed)
	}
	a, e := identitypg.BindSecurity(tx).Account(ctx, id)
	if e != nil {
		return e
	}
	if requireActive && a.Status != "active" {
		return live.Fail(apperror.Forbidden)
	}
	// Use generated Access query for the protected-owner catalog.
	owners, e := ownerIDs(ctx, tx)
	if e != nil {
		return e
	}
	for _, owner := range owners {
		if owner == id {
			return live.Fail(apperror.Forbidden)
		}
	}
	return nil
}

func ownerIDs(ctx context.Context, tx pgx.Tx) ([]string, error) { return accesssql.New(tx).Owners(ctx) }

// Association/status changes invalidate open accounting and enqueue a bounded
// batch of provider corrections before committing the administrative change.
func (r *Repository) invalidateMembers(ctx context.Context, q *livesql.Queries, account, agency string) error {
	rows, e := q.AffectedMembers(ctx, livesql.AffectedMembersParams{AccountID: account, AgencyID: agency})
	if e != nil {
		return e
	}
	if len(rows) > 500 {
		return live.Fail(apperror.Conflict)
	}
	accounts := []string{}
	ops := []livesql.EnqueueMembersParams{}
	for _, v := range rows {
		accounts = append(accounts, v.AccountID)
		ops = append(ops, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: v.RoomID, Kind: "participant", Target: v.AccountID})
	}
	if e := q.InvalidateMembers(ctx, accounts); e != nil {
		return e
	}
	_, e = q.EnqueueMembers(ctx, ops)
	return e
}
