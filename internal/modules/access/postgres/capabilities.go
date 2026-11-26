package postgres

import (
	"context"
	"encoding/json"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/restrictions"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Resolve is the one effective-capability engine. Call inside the existing
// authenticated security transaction. Room grants never escape their scope.
func Resolve(ctx context.Context, tx pgx.Tx, actor, room string) (out access.Effective, err error) {
	owners, e := accesssql.New(tx).Owners(ctx)
	if e != nil {
		return out, e
	}
	return resolveWithOwners(ctx, tx, actor, room, owners)
}

func resolveWithOwners(ctx context.Context, tx pgx.Tx, actor, room string, owners []string) (out access.Effective, err error) {
	out = access.Effective{Permissions: []string{}, Denied: []string{}}
	q := accesssql.New(tx)
	out.Owner = slices.Contains(owners, actor)
	policy, e := q.CapabilityPolicy(ctx, actor)
	if e != nil {
		return out, e
	}
	out.PolicyVersion = policy.PolicyID + ":" + strconv.FormatInt(policy.Revision, 10)
	grants, denied := map[string]bool{"room.enter": true, "packages.purchase": true}, map[string]bool{}
	if out.Owner {
		for _, p := range access.Catalog() {
			grants[p] = true
		}
	} else {
		base, e := q.Permissions(ctx, actor)
		if e != nil {
			return out, e
		}
		for _, p := range base {
			grants[p] = true
		}
		powers, e := q.CapabilityPackages(ctx, actor)
		if e != nil {
			return out, e
		}
		for _, p := range powers {
			if access.PackagePower(p) {
				grants[p] = true
			}
		}
	}
	var resource accesssql.CapabilityRoomRoleRow
	if room != "" {
		resource, e = q.CapabilityRoomRole(ctx, accesssql.CapabilityRoomRoleParams{ID: room, AccountID: actor})
		if e != nil {
			return out, e
		}
		if !out.Owner {
			role := resource.Role
			if resource.OwnerID == actor {
				role = "host"
			}
			var roles map[string][]string
			if e = json.Unmarshal(policy.Roles, &roles); e != nil {
				return out, e
			}
			for _, p := range roles[role] {
				grants[p] = true
			}
		}
	}
	if !out.Owner {
		overrides, e := q.CapabilityOverrides(ctx, accesssql.CapabilityOverridesParams{AccountID: actor, RoomID: room})
		if e != nil {
			return out, e
		}
		for _, o := range overrides {
			if !o.Allowed {
				denied[o.Action] = true
			} else {
				grants[o.Action] = true
			}
		}
		if grants["live.rooms.moderate"] && !denied["live.rooms.moderate"] && room != "" {
			for _, p := range access.RoomDefaults()["host"] {
				grants[p] = true
			}
		}
		for p := range denied {
			delete(grants, p)
		}
	}
	// Mandatory participation restrictions dominate every ordinary source, including
	// protected-owner grants. Protected configuration authority remains separate.
	blocked := policy.ActorLiveBanned || !grants["room.enter"]
	if room != "" {
		blocked = blocked || resource.RoomState != "open" || resource.MemberState == "banned" || resource.MemberState == "kicked" || (resource.Visibility != "public" && resource.OwnerID != actor && (resource.MemberState != "active" || resource.PrivateEntry) && !grants["room.enter.private"])
	}
	if blocked {
		for _, p := range []string{"room.enter", "room.enter.private", "room.join.hidden", "room.seat.self_join"} {
			delete(grants, p)
			denied[p] = true
		}
	}
	if room != "" && resource.RoomState != "open" {
		for p := range grants {
			if strings.HasPrefix(p, "room.") && p != "room.close" && p != "room.moderation_history.read" {
				delete(grants, p)
			}
		}
	}
	for p := range grants {
		out.Permissions = append(out.Permissions, p)
	}
	for p := range denied {
		out.Denied = append(out.Denied, p)
	}
	slices.Sort(out.Permissions)
	slices.Sort(out.Denied)
	return out, nil
}

func (r *Repository) Powers(ctx context.Context, c access.Credentials, room string) (out access.Effective, err error) {
	if room != "" && !access.ValidID(room) {
		return out, access.Failure(apperror.ValidationFailed)
	}
	err = r.Authorized(ctx, c, "", false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		var e error
		out, e = Resolve(ctx, tx, u.Actor(), room)
		return e
	})
	return
}
func (r *Repository) CapabilityPolicy(ctx context.Context, c access.Credentials, in *access.PolicyChange) (out any, err error) {
	if in != nil {
		defaults := access.RoomDefaults()
		if !access.ValidID(in.Previous) || !access.ValidReason(in.Reason) || len(in.Roles) != len(defaults) {
			return nil, access.Failure(apperror.ValidationFailed)
		}
		for role, actions := range in.Roles {
			if _, ok := defaults[role]; !ok || actions == nil || len(actions) > 32 {
				return nil, access.Failure(apperror.ValidationFailed)
			}
			seen := map[string]bool{}
			for _, p := range actions {
				if !slices.Contains(access.RoomPolicyActions(), p) || seen[p] {
					return nil, access.Failure(apperror.ValidationFailed)
				}
				seen[p] = true
			}
		}
	}
	err = r.Authorized(ctx, c, "capabilities.manage", false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := accesssql.New(tx)
		v, e := q.CapabilityPolicy(ctx, u.Actor())
		if e != nil {
			return e
		}
		if in == nil {
			var roles map[string][]string
			if e = json.Unmarshal(v.Roles, &roles); e != nil {
				return e
			}
			out = map[string]any{"policy_id": v.PolicyID, "revision": v.Revision, "roles": roles}
			return nil
		}
		if v.PolicyID != in.Previous {
			return access.Failure(apperror.Conflict)
		}
		b, e := json.Marshal(in.Roles)
		if e != nil {
			return e
		}
		id := access.ID()
		if e = q.InsertCapabilityPolicy(ctx, accesssql.InsertCapabilityPolicyParams{ID: id, PreviousID: pgtype.Text{String: in.Previous, Valid: true}, Roles: string(b)}); e != nil {
			return e
		}
		revision, e := q.SwitchCapabilityPolicy(ctx, accesssql.SwitchCapabilityPolicyParams{PolicyID: id, PolicyID_2: in.Previous})
		if e != nil {
			return e
		}
		out = map[string]any{"policy_id": id, "revision": revision, "roles": in.Roles}
		return u.Audit(ctx, access.Audit{Actor: u.Actor(), Target: id, Action: "capabilities.policy.publish", Reason: in.Reason, Correlation: access.ID(), Fields: []string{"previous:" + in.Previous}})
	})
	return out, mapError(err)
}
func (r *Repository) Override(ctx context.Context, c access.Credentials, target string, in access.OverrideChange) error {
	if in.RoomID != "" && !strings.HasPrefix(in.Action, "room.") {
		return access.Failure(apperror.ValidationFailed)
	}
	if !access.ValidID(target) || in.RoomID != "" && !access.ValidID(in.RoomID) || !slices.Contains(access.Catalog(), in.Action) || !access.ValidReason(in.Reason) || !in.Remove && (in.ExpiresAt.Before(time.Now()) || in.ExpiresAt.After(time.Now().Add(366*24*time.Hour))) {
		return access.Failure(apperror.ValidationFailed)
	}
	return r.Authorized(ctx, c, "", true, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		owners, e := accesssql.New(tx).Owners(ctx)
		if e != nil {
			return e
		}
		if slices.Contains(owners, target) {
			return access.Failure(apperror.Forbidden)
		}
		q := accesssql.New(tx)
		if in.RoomID != "" {
			if _, e = q.CapabilityRoomRole(ctx, accesssql.CapabilityRoomRoleParams{ID: in.RoomID, AccountID: target}); e != nil {
				return e
			}
		}
		if in.Remove {
			e = q.DeleteOverride(ctx, accesssql.DeleteOverrideParams{AccountID: target, RoomID: in.RoomID, Action: in.Action})
		} else {
			e = q.PutOverride(ctx, accesssql.PutOverrideParams{AccountID: target, RoomID: in.RoomID, Action: in.Action, Allowed: in.Allowed, ExpiresAt: pgtype.Timestamptz{Time: in.ExpiresAt, Valid: true}})
		}
		if e != nil {
			return e
		}
		if !in.Remove && !in.Allowed && in.Action == "room.enter" {
			if e = restrictions.CutParticipation(ctx, tx, target, in.RoomID); e != nil {
				return e
			}
		}
		if e = q.BumpCapabilityRevision(ctx); e != nil {
			return e
		}
		return u.Audit(ctx, access.Audit{Actor: u.Actor(), Target: target, Action: "capabilities.override", Reason: in.Reason, Correlation: access.ID(), Fields: []string{in.Action, "room:" + in.RoomID, "allowed:" + strconv.FormatBool(in.Allowed), "removed:" + strconv.FormatBool(in.Remove), "expires_at:" + in.ExpiresAt.UTC().Format(time.RFC3339Nano)}})
	})
}

// CreateModeratorRole uses existing protected-owner staff administration. The
// display name carries no authority; only this explicit capability set does.
func (r *Repository) CreateModeratorRole(ctx context.Context, c access.Credentials, name, reason, correlation string) (access.Role, error) {
	s, e := access.New(r)
	if e != nil {
		return access.Role{}, e
	}
	return s.PutRole(ctx, c, "", name, access.ModeratorDefaults(), true, reason, correlation)
}
