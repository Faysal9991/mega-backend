// Package access owns staff authorization and audited administration policy.
package access

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	UsersRead     = "users.read"
	PrivateRead   = "users.private.read"
	ProfileUpdate = "users.profile.update"
	Restrict      = "users.restrict"
)

func Catalog() []string {
	return []string{UsersRead, PrivateRead, ProfileUpdate, Restrict, "economy.policies.read", "economy.policies.write", "wallet.operations.read", "wallet.adjust", "withdrawals.review", "agency.hosts.manage", "agencies.manage", "live.rooms.moderate", "live.activity.read", "live.policies.manage"}
}
func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("access: entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func ValidID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func Failure(code apperror.Code) error { return apperror.New(code, nil) }
func Safe(err error) error {
	if err == nil {
		return nil
	}
	var a *apperror.Error
	if errors.As(err, &a) {
		return a
	}
	return Failure(apperror.ServiceUnavailable)
}

type Credentials struct{ Access, Installation string }
type Effective struct {
	Permissions []string `json:"permissions"`
	Owner       bool     `json:"protected_owner"`
}
type User struct {
	profiles.Profile
	Status string `json:"status"`
}
type Private struct {
	ContactEmail  string `json:"contact_email"`
	Username      string `json:"username"`
	Phone         string `json:"phone"`
	PhoneVerified bool   `json:"phone_verified"`
}
type Role struct {
	ID          string   `json:"role_id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}
type Page struct {
	Users []User
	Next  string
	More  bool
	Limit int
}
type Audit struct {
	Actor, Target, Action, Reason, Correlation string
	Fields                                     []string
}
type Unit interface {
	Actor() string
	Effective() Effective
	Profile(context.Context, string) (profiles.Profile, error)
	UpdateProfile(context.Context, string, profiles.Patch) (profiles.Profile, error)
	User(context.Context, string) (User, error)
	Private(context.Context, string) (Private, error)
	Directory(context.Context, string, int) ([]User, error)
	Roles(context.Context) ([]Role, error)
	PutRole(context.Context, Role, bool) error
	DeleteRole(context.Context, string) error
	Assignments(context.Context, string) ([]string, error)
	Assign(context.Context, string, []string) error
	Restrict(context.Context, string, bool) error
	Audit(context.Context, Audit) error
}
type Repository interface {
	Within(context.Context, Credentials, string, bool, string, func(Unit) error) error
}
type Service struct{ repo Repository }

func New(repo Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("access: repository required")
	}
	return &Service{repo}, nil
}
func (s *Service) within(ctx context.Context, c Credentials, permission string, owner bool, target string, fn func(Unit) error) error {
	if target != "" && !ValidID(target) {
		return Failure(apperror.ValidationFailed)
	}
	return Safe(s.repo.Within(ctx, c, permission, owner, target, fn))
}
func (s *Service) Profile(ctx context.Context, c Credentials, id string) (out profiles.Profile, err error) {
	err = s.within(ctx, c, "", false, id, func(u Unit) error {
		if id == "" {
			id = u.Actor()
		}
		var e error
		out, e = u.Profile(ctx, id)
		return e
	})
	return
}
func (s *Service) Permissions(ctx context.Context, c Credentials) (out Effective, err error) {
	err = s.within(ctx, c, "", false, "", func(u Unit) error { out = u.Effective(); return nil })
	return
}
func (s *Service) User(ctx context.Context, c Credentials, id string) (out User, err error) {
	err = s.within(ctx, c, UsersRead, false, id, func(u Unit) error { var e error; out, e = u.User(ctx, id); return e })
	return
}
func (s *Service) Private(ctx context.Context, c Credentials, id string) (out Private, err error) {
	err = s.within(ctx, c, PrivateRead, false, id, func(u Unit) error { var e error; out, e = u.Private(ctx, id); return e })
	return
}
func (s *Service) Directory(ctx context.Context, c Credentials, cursor string, limit int) (out Page, err error) {
	if limit < 1 || limit > 100 || cursor != "" && !ValidID(cursor) {
		return out, Failure(apperror.ValidationFailed)
	}
	err = s.within(ctx, c, UsersRead, false, "", func(u Unit) error {
		rows, e := u.Directory(ctx, cursor, limit+1)
		if e != nil {
			return e
		}
		out = Page{Users: rows, Limit: limit}
		if len(rows) > limit {
			out.Users = rows[:limit]
			out.More = true
			out.Next = rows[limit-1].AccountID
		}
		return nil
	})
	return
}
func validText(s string, max int) bool {
	if len(s) < 1 || len(s) > max || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validAudit(reason, correlation string) bool {
	if !validText(reason, 256) || len(correlation) < 1 || len(correlation) > 64 {
		return false
	}
	for _, r := range correlation {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func audit(ctx context.Context, u Unit, target, action, reason, correlation string, fields []string) error {
	return u.Audit(ctx, Audit{Actor: u.Actor(), Target: target, Action: action, Reason: reason, Correlation: correlation, Fields: fields})
}
func (s *Service) UpdateProfile(ctx context.Context, c Credentials, id string, p profiles.Patch, reason, correlation string) (out profiles.Profile, err error) {
	if e := p.Validate(); e != nil {
		return out, e
	}
	admin := id != ""
	permission := ""
	if admin {
		permission = ProfileUpdate
		if !validAudit(reason, correlation) {
			return out, Failure(apperror.ValidationFailed)
		}
	}
	err = s.within(ctx, c, permission, false, id, func(u Unit) error {
		if id == "" {
			id = u.Actor()
		}
		var e error
		out, e = u.UpdateProfile(ctx, id, p)
		if e != nil {
			return e
		}
		if admin {
			return audit(ctx, u, id, "users.profile.update", reason, correlation, p.Fields())
		}
		return nil
	})
	return
}
func (s *Service) SetRestriction(ctx context.Context, c Credentials, id string, restricted bool, reason, correlation string) error {
	if !validAudit(reason, correlation) {
		return Failure(apperror.ValidationFailed)
	}
	return s.within(ctx, c, Restrict, false, id, func(u Unit) error {
		if e := u.Restrict(ctx, id, restricted); e != nil {
			return e
		}
		field := "active"
		if restricted {
			field = "restricted"
		}
		return audit(ctx, u, id, "users.restrict", reason, correlation, []string{field})
	})
}
func (s *Service) Roles(ctx context.Context, c Credentials) (out []Role, err error) {
	err = s.within(ctx, c, "", true, "", func(u Unit) error { var e error; out, e = u.Roles(ctx); return e })
	return
}
func (s *Service) PutRole(ctx context.Context, c Credentials, id, name string, permissions []string, create bool, reason, correlation string) (out Role, err error) {
	if !validText(name, 64) || !validAudit(reason, correlation) || permissions == nil || len(permissions) > len(Catalog()) {
		return out, Failure(apperror.ValidationFailed)
	}
	seen := map[string]bool{}
	for _, p := range permissions {
		if !slices.Contains(Catalog(), p) || seen[p] {
			return out, Failure(apperror.ValidationFailed)
		}
		seen[p] = true
	}
	if seen[PrivateRead] && !seen[UsersRead] {
		return out, Failure(apperror.ValidationFailed)
	}
	if create {
		id = ID()
	}
	if !ValidID(id) {
		return out, Failure(apperror.ValidationFailed)
	}
	permissions = slices.Clone(permissions)
	slices.Sort(permissions)
	out = Role{id, name, permissions}
	err = s.within(ctx, c, "", true, "", func(u Unit) error {
		if e := u.PutRole(ctx, out, create); e != nil {
			return e
		}
		return audit(ctx, u, id, "roles.update", reason, correlation, append([]string{"name", "permissions"}, permissions...))
	})
	return
}
func (s *Service) DeleteRole(ctx context.Context, c Credentials, id, reason, correlation string) error {
	if !ValidID(id) || !validAudit(reason, correlation) {
		return Failure(apperror.ValidationFailed)
	}
	return s.within(ctx, c, "", true, "", func(u Unit) error {
		if e := u.DeleteRole(ctx, id); e != nil {
			return e
		}
		return audit(ctx, u, id, "roles.delete", reason, correlation, []string{"role_id"})
	})
}
func (s *Service) Assignments(ctx context.Context, c Credentials, id string) (out []string, err error) {
	err = s.within(ctx, c, "", true, id, func(u Unit) error { var e error; out, e = u.Assignments(ctx, id); return e })
	return
}
func (s *Service) Assign(ctx context.Context, c Credentials, id string, roles []string, reason, correlation string) error {
	if !validAudit(reason, correlation) || roles == nil || len(roles) > 100 {
		return Failure(apperror.ValidationFailed)
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if !ValidID(r) || seen[r] {
			return Failure(apperror.ValidationFailed)
		}
		seen[r] = true
	}
	return s.within(ctx, c, "", true, id, func(u Unit) error {
		if id == u.Actor() {
			return Failure(apperror.Forbidden)
		}
		if e := u.Assign(ctx, id, roles); e != nil {
			return e
		}
		return audit(ctx, u, id, "roles.assign", reason, correlation, append([]string{"role_ids"}, roles...))
	})
}

func ValidReason(s string) bool      { return validText(s, 256) }
func ValidCorrelation(s string) bool { return requestmeta.ValidID(s) }
