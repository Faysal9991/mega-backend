// Package postgres is the sole runtime writer of profile tables.
package postgres

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles/postgres/profilessql"
)

type Store struct{ q *profilessql.Queries }

func Bind(db profilessql.DBTX) *Store { return &Store{profilessql.New(db)} }
func (s *Store) Initialize(ctx context.Context, id, name, avatar string) error {
	return s.q.Initialize(ctx, profilessql.InitializeParams{AccountID: id, DisplayName: name, AvatarUrl: avatar})
}
func (s *Store) Read(ctx context.Context, id string) (profiles.Profile, error) {
	v, e := s.q.Read(ctx, id)
	return profiles.Profile{AccountID: v.AccountID, DisplayName: v.DisplayName, Bio: v.Bio, AvatarURL: v.AvatarUrl}, e
}
func (s *Store) Update(ctx context.Context, id string, p profiles.Patch) (profiles.Profile, error) {
	if e := p.Validate(); e != nil {
		return profiles.Profile{}, e
	}
	in := profilessql.UpdateParams{AccountID: id, SetName: p.DisplayName != nil, SetBio: p.Bio != nil}
	if p.DisplayName != nil {
		in.DisplayName = *p.DisplayName
	}
	if p.Bio != nil {
		in.Bio = *p.Bio
	}
	v, e := s.q.Update(ctx, in)
	return profiles.Profile{AccountID: v.AccountID, DisplayName: v.DisplayName, Bio: v.Bio, AvatarURL: v.AvatarUrl}, e
}

// AttachAvatar accepts only a server-validated approved asset URL supplied by the
// media application in the same authorized attachment transaction.
func (s *Store) AttachAvatar(ctx context.Context, id, url string) error {
	return s.q.AttachAvatar(ctx, profilessql.AttachAvatarParams{AccountID: id, AvatarUrl: url})
}
