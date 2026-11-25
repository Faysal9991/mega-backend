// Package identity owns account authentication and session security policies.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

var (
	ErrMissing      = errors.New("identity: record not found")
	ErrConflict     = errors.New("identity: identifier conflict")
	ErrStorage      = errors.New("identity: security state unavailable")
	usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)
	phonePattern    = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	idPattern       = regexp.MustCompile(`^[0-9a-f]{32}$`)
	tokenPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

func fail(code apperror.Code) error { return apperror.New(code, nil) }
func safe(err error) error {
	if err == nil {
		return nil
	}
	var app *apperror.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, ErrMissing) {
		return fail(apperror.Unauthenticated)
	}
	if errors.Is(err, ErrConflict) {
		return fail(apperror.Conflict)
	}
	return fail(apperror.ServiceUnavailable)
}
func Digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func randomID() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic("identity: entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func secret() string {
	var b [32]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic("identity: entropy unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func ValidInstallation(s string) bool { return idPattern.MatchString(s) }
func validToken(s string) bool        { return tokenPattern.MatchString(s) }
func NormalizeIdentifier(s string) (string, string, error) {
	if len(s) > 128 {
		return "", "", fail(apperror.ValidationFailed)
	}
	if strings.HasPrefix(s, "+") {
		if phonePattern.MatchString(s) {
			return "phone", s, nil
		}
		return "", "", fail(apperror.ValidationFailed)
	}
	s = strings.Trim(s, " \t\r\n")
	for _, b := range []byte(s) {
		if b >= 128 {
			return "", "", fail(apperror.ValidationFailed)
		}
	}
	s = strings.ToLower(s)
	if !usernamePattern.MatchString(s) {
		return "", "", fail(apperror.ValidationFailed)
	}
	return "username", s, nil
}
func validPassword(s string) bool { return len(s) >= 12 && len(s) <= 128 && utf8.ValidString(s) }

type Account struct{ ID, Status, DisplayName, Email, Avatar string }
type Challenge struct {
	Hash, Installation, Purpose string
	Created, Expires            time.Time
}
type Proof struct{ Account, Installation, Purpose string }
type Session struct {
	ID, Account, Installation string
	Revoked, Consumed         bool
	Expires                   time.Time
}
type Principal struct {
	SessionID string
	Account   Account
}
type Alias struct{ Kind, Value string }
type GoogleIdentity struct{ Subject, DisplayName, Email, Avatar string }
type GoogleVerifier interface {
	Verify(context.Context, string, string, time.Time) (GoogleIdentity, error)
}
type PasswordHasher interface {
	Hash(context.Context, string) (string, error)
	Verify(context.Context, string, string) (bool, error)
}

// Store exposes identity-owned operations. Within must bind all operations to
// one transaction; no provider or password hashing is performed in its callback.
type Store interface {
	EnsureInstallation(context.Context, string) (bool, error)
	LockSubject(context.Context, string) error
	GoogleAccount(context.Context, string) (Account, error)
	CreateGoogle(context.Context, Account, string) error
	LockAccount(context.Context, string) (Account, error)
	Password(context.Context, string) (string, error)
	PutPassword(context.Context, string, string) error
	FindIdentifier(context.Context, string, string) (string, error)
	Identifiers(context.Context, string) ([]Alias, error)
	PutIdentifier(context.Context, string, string, string) error
	CreateChallenge(context.Context, Challenge) error
	Challenge(context.Context, string) (Challenge, error)
	ConsumeChallenge(context.Context, string) error
	CreateProof(context.Context, string, Proof) error
	Proof(context.Context, string) (Proof, error)
	ConsumeProof(context.Context, string, Proof) error
	CreateSession(context.Context, Session, string) error
	AddRefresh(context.Context, string, string) error
	RefreshSession(context.Context, string) (Session, error)
	ConsumeRefresh(context.Context, string) error
	RotateSession(context.Context, string, string) error
	Access(context.Context, string, string) (Principal, error)
	RevokeSession(context.Context, string) error
	RevokeAll(context.Context, string) error
	Rate(context.Context, string) (int32, error)
	Cleanup(context.Context) error
}
type Repository interface {
	Store
	Within(context.Context, func(Store) error) error
}
type Tokens struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
	AccountID string `json:"account_id"`
}
type GoogleResult struct {
	Tokens *Tokens
	Proof  string
}
type ChallengeResult struct {
	Token     string `json:"challenge_token"`
	Nonce     string `json:"nonce"`
	ExpiresIn int    `json:"expires_in"`
}
type Me struct {
	AccountID     string `json:"account_id"`
	DisplayName   string `json:"display_name"`
	Email         string `json:"contact_email"`
	Avatar        string `json:"avatar_url"`
	Username      string `json:"username"`
	Phone         string `json:"phone"`
	PhoneVerified bool   `json:"phone_verified"`
}
