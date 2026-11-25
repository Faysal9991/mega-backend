package identity

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"time"
)

type Service struct {
	repo      Repository
	google    GoogleVerifier
	passwords PasswordHasher
}

func New(repo Repository, google GoogleVerifier, passwords PasswordHasher) (*Service, error) {
	if repo == nil || google == nil || passwords == nil {
		return nil, errors.New("identity: explicit dependencies required")
	}
	return &Service{repo, google, passwords}, nil
}

// Abuse is durable and runs before any provider/hash work, independent of Redis.
func (s *Service) Abuse(ctx context.Context, ip, installation, identifier string, expensive bool) error {
	if !ValidInstallation(installation) {
		return fail(apperror.ValidationFailed)
	}
	budgets := []struct {
		key string
		max int32
	}{{"ip:" + ip, 30}, {"installation:" + installation, 10}}
	if identifier != "" {
		budgets = append(budgets, struct {
			key string
			max int32
		}{"identifier:" + identifier, 5})
	}
	if expensive {
		budgets = append(budgets, struct {
			key string
			max int32
		}{"expensive", 120})
	}
	for _, b := range budgets {
		n, err := s.repo.Rate(ctx, Digest(b.key))
		if err != nil {
			return safe(err)
		}
		if n > b.max {
			return fail(apperror.RateLimited)
		}
	}
	return safe(s.repo.Cleanup(ctx))
}
func availableInstallation(ctx context.Context, tx Store, id string) error {
	blocked, err := tx.EnsureInstallation(ctx, id)
	if err != nil {
		return err
	}
	if blocked {
		return fail(apperror.Forbidden)
	}
	return nil
}
func activeAccount(ctx context.Context, tx Store, id string) (Account, error) {
	a, err := tx.LockAccount(ctx, id)
	if err != nil {
		return a, err
	}
	if a.Status != "active" {
		return a, fail(apperror.Forbidden)
	}
	return a, nil
}
func (s *Service) Challenge(ctx context.Context, installation, purpose string) (ChallengeResult, error) {
	if !ValidInstallation(installation) || (purpose != "login" && purpose != "password" && purpose != "identifiers") {
		return ChallengeResult{}, fail(apperror.ValidationFailed)
	}
	token := secret()
	result := ChallengeResult{token, Digest(token), 300}
	err := s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		return tx.CreateChallenge(ctx, Challenge{Hash: Digest(token), Installation: installation, Purpose: purpose})
	})
	return result, safe(err)
}
func (s *Service) Google(ctx context.Context, installation, challengeToken, idToken string) (GoogleResult, error) {
	if !ValidInstallation(installation) || !validToken(challengeToken) {
		return GoogleResult{}, fail(apperror.Unauthenticated)
	}
	c, err := s.repo.Challenge(ctx, Digest(challengeToken))
	if err != nil {
		return GoogleResult{}, safe(err)
	}
	if c.Installation != installation {
		return GoogleResult{}, fail(apperror.Unauthenticated)
	}
	g, err := s.google.Verify(ctx, idToken, Digest(challengeToken), c.Created)
	if err != nil {
		return GoogleResult{}, safe(err)
	}
	var output GoogleResult
	err = s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		if err := tx.LockSubject(ctx, g.Subject); err != nil {
			return err
		}
		a, err := tx.GoogleAccount(ctx, g.Subject)
		if errors.Is(err, ErrMissing) && c.Purpose == "login" {
			a = Account{ID: randomID(), Status: "active", DisplayName: g.DisplayName, Email: g.Email, Avatar: g.Avatar}
			err = tx.CreateGoogle(ctx, a, g.Subject)
		}
		if err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, a.ID); err != nil {
			return err
		}
		if err := tx.ConsumeChallenge(ctx, c.Hash); err != nil {
			return err
		}
		if c.Purpose == "login" {
			tokens, err := issue(ctx, tx, a.ID, installation)
			output.Tokens = &tokens
			return err
		}
		proof := secret()
		output.Proof = proof
		return tx.CreateProof(ctx, Digest(proof), Proof{a.ID, installation, c.Purpose})
	})
	return output, safe(err)
}
func issue(ctx context.Context, tx Store, account, installation string) (Tokens, error) {
	t := Tokens{Access: secret(), Refresh: secret(), ExpiresIn: 900, AccountID: account}
	session := Session{ID: randomID(), Account: account, Installation: installation}
	if err := tx.CreateSession(ctx, session, Digest(t.Access)); err != nil {
		return Tokens{}, err
	}
	if err := tx.AddRefresh(ctx, Digest(t.Refresh), session.ID); err != nil {
		return Tokens{}, err
	}
	return t, nil
}
func (s *Service) PasswordLogin(ctx context.Context, installation, identifier, password string) (Tokens, error) {
	kind, value, err := NormalizeIdentifier(identifier)
	if err != nil || !ValidInstallation(installation) || len(password) > 128 {
		return Tokens{}, fail(apperror.Unauthenticated)
	}
	id, lookupErr := s.repo.FindIdentifier(ctx, kind, value)
	if lookupErr != nil && !errors.Is(lookupErr, ErrMissing) {
		return Tokens{}, safe(lookupErr)
	}
	hash := ""
	if id != "" {
		hash, err = s.repo.Password(ctx, id)
		if err != nil {
			return Tokens{}, safe(err)
		}
	}
	ok, err := s.passwords.Verify(ctx, password, hash)
	if err != nil {
		return Tokens{}, safe(err)
	}
	if !ok || lookupErr != nil {
		return Tokens{}, fail(apperror.Unauthenticated)
	}
	var tokens Tokens
	err = s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, id); err != nil {
			return err
		}
		current, err := tx.Password(ctx, id)
		if err != nil {
			return err
		}
		if current != hash {
			return fail(apperror.Unauthenticated)
		}
		// Alias reassignment during password verification must not authenticate the old owner.
		actual, err := tx.FindIdentifier(ctx, kind, value)
		if err != nil {
			return err
		}
		if actual != id {
			return fail(apperror.Unauthenticated)
		}
		tokens, err = issue(ctx, tx, id, installation)
		return err
	})
	return tokens, safe(err)
}
func (s *Service) Authenticate(ctx context.Context, access, installation string) (Principal, error) {
	if !validToken(access) || !ValidInstallation(installation) {
		return Principal{}, fail(apperror.Unauthenticated)
	}
	p, err := s.repo.Access(ctx, Digest(access), installation)
	return p, safe(err)
}
func (s *Service) Current(ctx context.Context, p Principal) (Me, error) {
	aliases, err := s.repo.Identifiers(ctx, p.Account.ID)
	if err != nil {
		return Me{}, safe(err)
	}
	m := Me{AccountID: p.Account.ID, DisplayName: p.Account.DisplayName, Email: p.Account.Email, Avatar: p.Account.Avatar}
	for _, alias := range aliases {
		if alias.Kind == "username" {
			m.Username = alias.Value
		} else if alias.Kind == "phone" {
			m.Phone = alias.Value
		}
	}
	return m, nil
}
func (s *Service) Identifiers(ctx context.Context, access, installation, username, phone, proofToken string) error {
	if username == "" && phone == "" {
		return fail(apperror.ValidationFailed)
	}
	var aliases []Alias
	for _, entry := range []struct{ raw, kind string }{{username, "username"}, {phone, "phone"}} {
		if entry.raw == "" {
			continue
		}
		kind, value, err := NormalizeIdentifier(entry.raw)
		if err != nil || kind != entry.kind {
			return fail(apperror.ValidationFailed)
		}
		aliases = append(aliases, Alias{kind, value})
	}
	p, err := s.Authenticate(ctx, access, installation)
	if err != nil {
		return err
	}
	if !validToken(proofToken) {
		return fail(apperror.Unauthenticated)
	}
	return safe(s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, p.Account.ID); err != nil {
			return err
		}
		if _, err := tx.Access(ctx, Digest(access), installation); err != nil {
			return err
		}
		if err := tx.ConsumeProof(ctx, Digest(proofToken), Proof{p.Account.ID, installation, "identifiers"}); err != nil {
			return err
		}
		for _, a := range aliases {
			if err := tx.PutIdentifier(ctx, p.Account.ID, a.Kind, a.Value); err != nil {
				return err
			}
		}
		return nil
	}))
}

type PasswordChange struct{ Installation, Password, ProofToken, AccessToken, CurrentPassword string }

func (s *Service) SetPassword(ctx context.Context, in PasswordChange) error {
	if !ValidInstallation(in.Installation) || !validPassword(in.Password) {
		return fail(apperror.ValidationFailed)
	}
	var account, old string
	var err error
	byProof := in.ProofToken != ""
	if byProof {
		if !validToken(in.ProofToken) || in.AccessToken != "" || in.CurrentPassword != "" {
			return fail(apperror.ValidationFailed)
		}
		proof, e := s.repo.Proof(ctx, Digest(in.ProofToken))
		if e != nil {
			return safe(e)
		}
		if proof.Installation != in.Installation || proof.Purpose != "password" {
			return fail(apperror.Unauthenticated)
		}
		account = proof.Account
	} else {
		p, e := s.Authenticate(ctx, in.AccessToken, in.Installation)
		if e != nil {
			return e
		}
		account = p.Account.ID
		old, err = s.repo.Password(ctx, account)
		if err != nil {
			return safe(err)
		}
		ok, e := s.passwords.Verify(ctx, in.CurrentPassword, old)
		if e != nil {
			return safe(e)
		}
		if !ok {
			return fail(apperror.Unauthenticated)
		}
	}
	hash, err := s.passwords.Hash(ctx, in.Password)
	if err != nil {
		return safe(err)
	}
	return safe(s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, in.Installation); err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, account); err != nil {
			return err
		}
		if byProof {
			if err := tx.ConsumeProof(ctx, Digest(in.ProofToken), Proof{account, in.Installation, "password"}); err != nil {
				return err
			}
		} else {
			if _, err := tx.Access(ctx, Digest(in.AccessToken), in.Installation); err != nil {
				return err
			}
			current, err := tx.Password(ctx, account)
			if err != nil {
				return err
			}
			if current != old {
				return fail(apperror.Unauthenticated)
			}
		}
		if err := tx.PutPassword(ctx, account, hash); err != nil {
			return err
		}
		return tx.RevokeAll(ctx, account)
	}))
}
func (s *Service) Refresh(ctx context.Context, installation, refresh string) (Tokens, error) {
	if !ValidInstallation(installation) || !validToken(refresh) {
		return Tokens{}, fail(apperror.Unauthenticated)
	}
	before, err := s.repo.RefreshSession(ctx, Digest(refresh))
	if err != nil {
		return Tokens{}, safe(err)
	}
	if before.Installation != installation {
		return Tokens{}, fail(apperror.Unauthenticated)
	}
	var tokens Tokens
	reused := false
	err = s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, before.Account); err != nil {
			return err
		}
		current, err := tx.RefreshSession(ctx, Digest(refresh))
		if err != nil {
			return err
		}
		if current.Consumed {
			reused = true
			return tx.RevokeSession(ctx, current.ID)
		}
		if current.Revoked || !time.Now().Before(current.Expires) {
			return fail(apperror.Unauthenticated)
		}
		if err := tx.ConsumeRefresh(ctx, Digest(refresh)); err != nil {
			return err
		}
		seconds := int(time.Until(current.Expires) / time.Second)
		if seconds > 900 {
			seconds = 900
		}
		if seconds < 1 {
			return fail(apperror.Unauthenticated)
		}
		tokens = Tokens{Access: secret(), Refresh: secret(), ExpiresIn: seconds, AccountID: current.Account}
		if err := tx.RotateSession(ctx, current.ID, Digest(tokens.Access)); err != nil {
			return err
		}
		return tx.AddRefresh(ctx, Digest(tokens.Refresh), current.ID)
	})
	if err != nil {
		return Tokens{}, safe(err)
	}
	if reused {
		return Tokens{}, fail(apperror.Unauthenticated)
	}
	return tokens, nil
}
func (s *Service) Logout(ctx context.Context, access, installation string, all bool) error {
	p, err := s.Authenticate(ctx, access, installation)
	if err != nil {
		return err
	}
	return safe(s.repo.Within(ctx, func(tx Store) error {
		if err := availableInstallation(ctx, tx, installation); err != nil {
			return err
		}
		if _, err := activeAccount(ctx, tx, p.Account.ID); err != nil {
			return err
		}
		if _, err := tx.Access(ctx, Digest(access), installation); err != nil {
			return err
		}
		if all {
			return tx.RevokeAll(ctx, p.Account.ID)
		}
		return tx.RevokeSession(ctx, p.SessionID)
	}))
}
