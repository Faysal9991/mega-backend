// Package postgres implements identity storage with generated parameterized SQL.
package postgres

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/identity/postgres/identitysql"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	profilepg "github.com/Faysal9991/mega-backend/internal/modules/profiles/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type store struct {
	q        *identitysql.Queries
	profiles profiles.Initializer
}
type Repository struct {
	*store
	tx *platform.Transactions
}

func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, identity.ErrStorage
	}
	tx, err := platform.NewTransactions(pool, time.Second)
	if err != nil {
		return nil, identity.ErrStorage
	}
	return &Repository{&store{q: identitysql.New(pool)}, tx}, nil
}
func mapped(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrMissing
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return identity.ErrConflict
	}
	return identity.ErrStorage
}
func (r *Repository) Within(ctx context.Context, fn func(identity.Store) error) error {
	err := r.tx.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(&store{q: identitysql.New(tx), profiles: profilepg.Bind(tx)})
	})
	// Transactions wrap callback errors safely; preserve their application codes.
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return mapped(err)
	}
	return err
}
func (s *store) EnsureInstallation(ctx context.Context, id string) (bool, error) {
	v, e := s.q.EnsureInstallation(ctx, id)
	return v, mapped(e)
}
func (s *store) LockSubject(ctx context.Context, sub string) error {
	return mapped(s.q.LockSubject(ctx, sub))
}
func (s *store) GoogleAccount(ctx context.Context, sub string) (identity.Account, error) {
	v, e := s.q.GoogleAccount(ctx, sub)
	return identity.Account{ID: v.ID, Status: v.Status, DisplayName: v.DisplayName, Email: v.ContactEmail, Avatar: v.AvatarUrl}, mapped(e)
}
func (s *store) CreateGoogle(ctx context.Context, a identity.Account, sub string) error {
	// Signup writes are available only on the transaction-bound adapter.
	if s.profiles == nil {
		return identity.ErrStorage
	}
	if s.profiles == nil {
		return identity.ErrStorage
	} // Creation requires the transaction-scoped profile writer.
	if err := s.q.CreateAccount(ctx, identitysql.CreateAccountParams{ID: a.ID, ContactEmail: a.Email}); err != nil {
		return mapped(err)
	}
	if err := s.q.LinkGoogle(ctx, identitysql.LinkGoogleParams{Subject: sub, AccountID: a.ID}); err != nil {
		return mapped(err)
	}
	return mapped(s.profiles.Initialize(ctx, a.ID, a.DisplayName, a.Avatar))
}
func (s *store) LockAccount(ctx context.Context, id string) (identity.Account, error) {
	v, e := s.q.LockAccount(ctx, id)
	return identity.Account{ID: v.ID, Status: v.Status, DisplayName: v.DisplayName, Email: v.ContactEmail, Avatar: v.AvatarUrl}, mapped(e)
}
func (s *store) Password(ctx context.Context, id string) (string, error) {
	v, e := s.q.Password(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil
	}
	return v, mapped(e)
}
func (s *store) PutPassword(ctx context.Context, id, hash string) error {
	return mapped(s.q.PutPassword(ctx, identitysql.PutPasswordParams{AccountID: id, Credential: hash}))
}
func (s *store) FindIdentifier(ctx context.Context, kind, value string) (string, error) {
	v, e := s.q.FindIdentifier(ctx, identitysql.FindIdentifierParams{Kind: kind, Value: value})
	return v, mapped(e)
}
func (s *store) Identifiers(ctx context.Context, id string) ([]identity.Alias, error) {
	v, e := s.q.Identifiers(ctx, id)
	if e != nil {
		return nil, mapped(e)
	}
	out := make([]identity.Alias, 0, len(v))
	for _, a := range v {
		out = append(out, identity.Alias{Kind: a.Kind, Value: a.Value})
	}
	return out, nil
}
func (s *store) PutIdentifier(ctx context.Context, id, kind, value string) error {
	return mapped(s.q.PutIdentifier(ctx, identitysql.PutIdentifierParams{AccountID: id, Kind: kind, Value: value}))
}
func (s *store) CreateChallenge(ctx context.Context, c identity.Challenge) error {
	return mapped(s.q.CreateChallenge(ctx, identitysql.CreateChallengeParams{TokenHash: c.Hash, InstallationID: c.Installation, Purpose: c.Purpose}))
}
func (s *store) Challenge(ctx context.Context, hash string) (identity.Challenge, error) {
	v, e := s.q.Challenge(ctx, hash)
	return identity.Challenge{Hash: v.TokenHash, Installation: v.InstallationID, Purpose: v.Purpose, Created: v.CreatedAt.Time, Expires: v.ExpiresAt.Time}, mapped(e)
}
func (s *store) ConsumeChallenge(ctx context.Context, hash string) error {
	_, e := s.q.ConsumeChallenge(ctx, hash)
	return mapped(e)
}
func (s *store) CreateProof(ctx context.Context, hash string, p identity.Proof) error {
	return mapped(s.q.CreateProof(ctx, identitysql.CreateProofParams{TokenHash: hash, AccountID: p.Account, InstallationID: p.Installation, Purpose: p.Purpose}))
}
func (s *store) Proof(ctx context.Context, hash string) (identity.Proof, error) {
	v, e := s.q.Proof(ctx, hash)
	return identity.Proof{Account: v.AccountID, Installation: v.InstallationID, Purpose: v.Purpose}, mapped(e)
}
func (s *store) ConsumeProof(ctx context.Context, hash string, p identity.Proof) error {
	_, e := s.q.ConsumeProof(ctx, identitysql.ConsumeProofParams{TokenHash: hash, AccountID: p.Account, InstallationID: p.Installation, Purpose: p.Purpose})
	return mapped(e)
}
func (s *store) CreateSession(ctx context.Context, v identity.Session, access string) error {
	if err := s.q.PruneSessions(ctx, v.Account); err != nil {
		return mapped(err)
	}
	return mapped(s.q.CreateSession(ctx, identitysql.CreateSessionParams{ID: v.ID, AccountID: v.Account, InstallationID: v.Installation, AccessHash: access}))
}
func (s *store) AddRefresh(ctx context.Context, hash, id string) error {
	return mapped(s.q.AddRefresh(ctx, identitysql.AddRefreshParams{TokenHash: hash, SessionID: id}))
}
func (s *store) RefreshSession(ctx context.Context, hash string) (identity.Session, error) {
	v, e := s.q.RefreshSession(ctx, hash)
	return identity.Session{ID: v.ID, Account: v.AccountID, Installation: v.InstallationID, Revoked: v.Revoked, Consumed: v.Consumed, Expires: v.ExpiresAt.Time}, mapped(e)
}
func (s *store) ConsumeRefresh(ctx context.Context, hash string) error {
	_, e := s.q.ConsumeRefresh(ctx, hash)
	return mapped(e)
}
func (s *store) RotateSession(ctx context.Context, id, hash string) error {
	return mapped(s.q.RotateSession(ctx, identitysql.RotateSessionParams{ID: id, AccessHash: hash}))
}
func (s *store) Access(ctx context.Context, hash, installation string) (identity.Principal, error) {
	v, e := s.q.Access(ctx, identitysql.AccessParams{AccessHash: hash, InstallationID: installation})
	return identity.Principal{SessionID: v.SessionID, Account: identity.Account{ID: v.AccountID, Status: "active", DisplayName: v.DisplayName, Email: v.ContactEmail, Avatar: v.AvatarUrl}}, mapped(e)
}
func (s *store) RevokeSession(ctx context.Context, id string) error {
	return mapped(s.q.RevokeSession(ctx, id))
}
func (s *store) RevokeAll(ctx context.Context, id string) error {
	return mapped(s.q.RevokeAll(ctx, id))
}
func (s *store) Rate(ctx context.Context, scope string) (int32, error) {
	v, e := s.q.Rate(ctx, scope)
	return v, mapped(e)
}
func (s *store) Cleanup(ctx context.Context) error {
	if err := s.q.CleanupRates(ctx); err != nil {
		return mapped(err)
	}
	if err := s.q.CleanupChallenges(ctx); err != nil {
		return mapped(err)
	}
	if err := s.q.CleanupProofs(ctx); err != nil {
		return mapped(err)
	}
	if err := s.q.CleanupRefreshes(ctx); err != nil {
		return mapped(err)
	}
	return mapped(s.q.CleanupSessions(ctx))
}

// Internal management interface; HTTP callers must use scoped administration services.
func (r *Repository) RestrictAccount(ctx context.Context, id string) error {
	return r.Within(ctx, func(tx identity.Store) error {
		if _, err := tx.LockAccount(ctx, id); err != nil {
			return err
		}
		st := tx.(*store)
		if err := st.q.RestrictAccount(ctx, id); err != nil {
			return mapped(err)
		}
		return tx.RevokeAll(ctx, id)
	})
}
func (r *Repository) RestrictInstallation(ctx context.Context, id string) error {
	return r.Within(ctx, func(tx identity.Store) error {
		if _, err := tx.EnsureInstallation(ctx, id); err != nil {
			return err
		}
		return mapped(tx.(*store).q.RestrictInstallation(ctx, id))
	})
}

var _ identity.Repository = (*Repository)(nil)

// Ready checks artifacts only; it never runs migrations or changes schema.
func (r *Repository) Ready(ctx context.Context) error {
	ready, err := r.q.SchemaReady(ctx)
	if err != nil || !ready {
		return identity.ErrStorage
	}
	return nil
}

// Security binds Identity-owned account/session operations to a caller transaction.
// Lock installation before lexically ordered accounts, matching identity flows.
type Security struct{ s *store }

func BindSecurity(tx pgx.Tx) *Security {
	return &Security{s: &store{q: identitysql.New(tx), profiles: profilepg.Bind(tx)}}
}
func (s *Security) Installation(ctx context.Context, id string) error {
	restricted, e := s.s.EnsureInstallation(ctx, id)
	if e != nil {
		return e
	}
	if restricted {
		return apperror.New(apperror.Forbidden, nil)
	}
	return nil
}
func (s *Security) Account(ctx context.Context, id string) (identity.Account, error) {
	return s.s.LockAccount(ctx, id)
}
func (s *Security) Access(ctx context.Context, hash, installation string) (identity.Principal, error) {
	return s.s.Access(ctx, hash, installation)
}
func (s *Security) SetRestriction(ctx context.Context, id string, restricted bool) error {
	if e := s.s.q.SetRestriction(ctx, identitysql.SetRestrictionParams{ID: id, Column2: restricted}); e != nil {
		return e
	}
	if restricted {
		return s.s.RevokeAll(ctx, id)
	}
	return nil
}
