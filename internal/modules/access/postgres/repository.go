// Package postgres composes module-owned transaction adapters for authorization.
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	profilepg "github.com/Faysal9991/mega-backend/internal/modules/profiles/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"slices"
	"time"
)

type Repository struct {
	pool         *pgxpool.Pool
	transactions *platform.Transactions
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, access.Failure(apperror.ServiceUnavailable)
	}
	tx, e := platform.NewTransactions(pool, time.Second)
	if e != nil {
		return nil, e
	}
	return &Repository{pool, tx}, nil
}

type unit struct {
	q                  *accesssql.Queries
	transaction        pgx.Tx
	security           *identitypg.Security
	profiles           *profilepg.Store
	actor              string
	effective          access.Effective
	owners             []string
	privilegedMutation bool
	auditWritten       bool
}

func mapError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, identity.ErrMissing) {
		return access.Failure(apperror.NotFound)
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "23505" || p.Code == "23503") {
		return access.Failure(apperror.Conflict)
	}
	return access.Safe(e)
}
func (r *Repository) Within(ctx context.Context, c access.Credentials, permission string, owner bool, target string, fn func(access.Unit) error) error {
	if fn == nil {
		return access.Failure(apperror.ServiceUnavailable)
	}
	if len(c.Access) != 43 || !identity.ValidInstallation(c.Installation) {
		return access.Failure(apperror.Unauthenticated)
	}
	// First lookup only discovers immutable account ID; all authority is rechecked below.
	auth, e := identitypg.New(r.pool)
	if e != nil {
		return mapError(e)
	}
	p, e := auth.Access(ctx, identity.Digest(c.Access), c.Installation)
	if e != nil {
		if errors.Is(e, identity.ErrMissing) {
			return access.Failure(apperror.Unauthenticated)
		}
		return mapError(e)
	}
	attempts, e := auth.Rate(ctx, identity.Digest("profiles-access:"+p.Account.ID))
	if e != nil {
		return mapError(e)
	}
	if attempts > 120 {
		return access.Failure(apperror.RateLimited)
	}
	e = r.transactions.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		q := accesssql.New(tx)
		if e := q.LockSecurity(ctx); e != nil {
			return e
		}
		security := identitypg.BindSecurity(tx)
		if e := security.Installation(ctx, c.Installation); e != nil {
			return e
		}
		actual, e := security.Access(ctx, identity.Digest(c.Access), c.Installation)
		if e != nil {
			if errors.Is(e, identity.ErrMissing) {
				return access.Failure(apperror.Unauthenticated)
			}
			return e
		}
		owners, e := q.Owners(ctx)
		if e != nil {
			return e
		}
		permissions, e := q.Permissions(ctx, actual.Account.ID)
		if e != nil {
			return e
		}
		effective := access.Effective{Permissions: permissions, Owner: slices.Contains(owners, actual.Account.ID)}
		if effective.Owner {
			effective.Permissions = access.Catalog()
		}
		if owner && !effective.Owner || permission != "" && !slices.Contains(effective.Permissions, permission) {
			return access.Failure(apperror.Forbidden)
		}
		if permission == "users.private.read" && !slices.Contains(effective.Permissions, "users.read") {
			return access.Failure(apperror.Forbidden)
		}
		ids := []string{p.Account.ID}
		if target != "" && target != p.Account.ID {
			ids = append(ids, target)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if _, e := security.Account(ctx, id); e != nil {
				return e
			}
		}
		if _, e := security.Access(ctx, identity.Digest(c.Access), c.Installation); e != nil {
			if errors.Is(e, identity.ErrMissing) {
				return access.Failure(apperror.Unauthenticated)
			}
			return e
		}
		u := &unit{transaction: tx, q: q, security: security, profiles: profilepg.Bind(tx), actor: actual.Account.ID, effective: effective, owners: owners}
		if e := fn(u); e != nil {
			return e
		}
		if u.privilegedMutation && !u.auditWritten {
			return access.Failure(apperror.ServiceUnavailable)
		}
		return nil
	})
	return mapError(e)
}
func (u *unit) Actor() string               { return u.actor }
func (u *unit) Effective() access.Effective { return u.effective }
func (u *unit) Profile(ctx context.Context, id string) (profiles.Profile, error) {
	return u.profiles.Read(ctx, id)
}
func (u *unit) UpdateProfile(ctx context.Context, id string, p profiles.Patch) (profiles.Profile, error) {
	if id != u.actor {
		if e := u.require(access.ProfileUpdate); e != nil {
			return profiles.Profile{}, e
		}
	}
	if id != u.actor {
		u.privilegedMutation = true
	}
	return u.profiles.Update(ctx, id, p)
}
func (u *unit) User(ctx context.Context, id string) (access.User, error) {
	if e := u.require(access.UsersRead); e != nil {
		return access.User{}, e
	}
	v, e := u.q.User(ctx, id)
	return access.User{Profile: profiles.Profile{AccountID: v.AccountID, DisplayName: v.DisplayName, Bio: v.Bio, AvatarURL: v.AvatarUrl}, Status: v.Status}, e
}
func (u *unit) Private(ctx context.Context, id string) (access.Private, error) {
	if e := u.require(access.PrivateRead, access.UsersRead); e != nil {
		return access.Private{}, e
	}
	v, e := u.q.Private(ctx, id)
	return access.Private{ContactEmail: v.ContactEmail, Username: v.Username, Phone: v.Phone}, e
}
func (u *unit) Directory(ctx context.Context, cursor string, limit int) ([]access.User, error) {
	if e := u.require(access.UsersRead); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 101 {
		return nil, access.Failure(apperror.ValidationFailed)
	}
	rows, e := u.q.Directory(ctx, accesssql.DirectoryParams{ID: cursor, Limit: int32(limit)})
	if e != nil {
		return nil, e
	}
	out := make([]access.User, 0, len(rows))
	for _, v := range rows {
		out = append(out, access.User{Profile: profiles.Profile{AccountID: v.AccountID, DisplayName: v.DisplayName, Bio: v.Bio, AvatarURL: v.AvatarUrl}, Status: v.Status})
	}
	return out, nil
}
func (u *unit) Roles(ctx context.Context) ([]access.Role, error) {
	if !u.effective.Owner {
		return nil, access.Failure(apperror.Forbidden)
	}
	rows, e := u.q.Roles(ctx)
	if e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, access.Failure(apperror.Conflict)
	}
	out := make([]access.Role, 0, len(rows))
	for _, v := range rows {
		out = append(out, access.Role{ID: v.ID, Name: v.Name, Permissions: v.Permissions})
	}
	return out, nil
}
func (u *unit) PutRole(ctx context.Context, r access.Role, create bool) error {
	u.privilegedMutation = true
	if !u.effective.Owner {
		return access.Failure(apperror.Forbidden)
	}
	exists, e := u.q.RoleExists(ctx, r.ID)
	if e != nil {
		return e
	}
	if create && exists {
		return access.Failure(apperror.Conflict)
	}
	if !create && !exists {
		return access.Failure(apperror.NotFound)
	}
	if create {
		roles, e := u.Roles(ctx)
		if e != nil {
			return e
		}
		if len(roles) >= 100 {
			return access.Failure(apperror.Conflict)
		}
	}
	if e := u.q.PutRole(ctx, accesssql.PutRoleParams{ID: r.ID, Name: r.Name}); e != nil {
		return e
	}
	if e := u.q.DeletePermissions(ctx, r.ID); e != nil {
		return e
	}
	for _, p := range r.Permissions {
		if e := u.q.AddPermission(ctx, accesssql.AddPermissionParams{RoleID: r.ID, Permission: p}); e != nil {
			return e
		}
	}
	return nil
}
func (u *unit) DeleteRole(ctx context.Context, id string) error {
	u.privilegedMutation = true
	if !u.effective.Owner {
		return access.Failure(apperror.Forbidden)
	}
	_, e := u.q.DeleteRole(ctx, id)
	return e
}
func (u *unit) Assignments(ctx context.Context, id string) ([]string, error) {
	if !u.effective.Owner {
		return nil, access.Failure(apperror.Forbidden)
	}
	rows, e := u.q.Assignments(ctx, id)
	if e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, access.Failure(apperror.Conflict)
	}
	return rows, nil
}
func (u *unit) Assign(ctx context.Context, id string, roles []string) error {
	u.privilegedMutation = true
	if !u.effective.Owner {
		return access.Failure(apperror.Forbidden)
	}
	if len(roles) > 100 {
		return access.Failure(apperror.ValidationFailed)
	}
	if slices.Contains(u.owners, id) || id == u.actor {
		return access.Failure(apperror.Forbidden)
	}
	if e := u.q.ClearAssignments(ctx, id); e != nil {
		return e
	}
	for _, role := range roles {
		if e := u.q.Assign(ctx, accesssql.AssignParams{AccountID: id, RoleID: role}); e != nil {
			return e
		}
	}
	return nil
}
func (u *unit) Restrict(ctx context.Context, id string, restricted bool) error {
	u.privilegedMutation = true
	if e := u.require(access.Restrict); e != nil {
		return e
	}
	if slices.Contains(u.owners, id) {
		return access.Failure(apperror.Forbidden)
	}
	return u.security.SetRestriction(ctx, id, restricted)
}
func newID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("access: entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (u *unit) Audit(ctx context.Context, a access.Audit) error {
	if a.Actor != u.actor {
		return access.Failure(apperror.Forbidden)
	}
	err := u.q.Audit(ctx, accesssql.AuditParams{ID: newID(), Actor: a.Actor, Target: a.Target, Action: a.Action, Fields: a.Fields, Reason: a.Reason, Correlation: a.Correlation})
	if err == nil {
		u.auditWritten = true
	}
	return err
}

// Bootstrap is an operator-only path: runtime credentials cannot insert owners.
// The same database advisory lock orders it with all privileged API actions.
func Bootstrap(ctx context.Context, tx pgx.Tx, account, reason, correlation string) error {
	if !access.ValidID(account) || !access.ValidReason(reason) || !access.ValidCorrelation(correlation) {
		return access.Failure(apperror.ValidationFailed)
	}
	q := accesssql.New(tx)
	if e := q.LockSecurity(ctx); e != nil {
		return e
	}
	owners, e := q.Owners(ctx)
	if e != nil {
		return e
	}
	if len(owners) != 0 {
		return access.Failure(apperror.Conflict)
	}
	security := identitypg.BindSecurity(tx)
	a, e := security.Account(ctx, account)
	if e != nil {
		return e
	}
	linked, e := q.GoogleLinked(ctx, account)
	if e != nil {
		return e
	}
	if a.Status != "active" || !linked {
		return access.Failure(apperror.Forbidden)
	}
	if e := q.Bootstrap(ctx, account); e != nil {
		return e
	}
	return q.Audit(ctx, accesssql.AuditParams{ID: newID(), Actor: account, Target: account, Action: "owner.bootstrap", Fields: []string{"protected_owner"}, Reason: reason, Correlation: correlation})
}

func (u *unit) require(permissions ...string) error {
	for _, p := range permissions {
		if !slices.Contains(u.effective.Permissions, p) {
			return access.Failure(apperror.Forbidden)
		}
	}
	return nil
}

// Financial binds a Wallet-owned callback to the same authoritative security
// transaction and advisory lock as staff mutations. Access still owns audit writes.
func (r *Repository) Financial(ctx context.Context, c access.Credentials, permission string, owner bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	return r.Within(ctx, c, permission, owner, target, func(u access.Unit) error { return fn(ctx, u.(*unit).transaction, u) })
}
