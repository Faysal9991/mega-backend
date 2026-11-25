package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"golang.org/x/crypto/argon2"
	"strings"
)

// Fixed parameters prevent attacker-controlled PHC costs or allocation sizes.
type ArgonPasswords struct {
	slots chan struct{}
	dummy string
}

func NewPasswords() *ArgonPasswords {
	p := &ArgonPasswords{slots: make(chan struct{}, 2)}
	p.dummy = encodeHash("not-a-user-password", make([]byte, 16))
	return p
}
func encodeHash(password string, salt []byte) string {
	key := argon2.IDKey([]byte(password), salt, 3, 65536, 2, 32)
	return "$argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}
func (p *ArgonPasswords) acquire(ctx context.Context) error {
	if ctx.Err() != nil {
		return fail(apperror.ServiceUnavailable)
	}
	select {
	case p.slots <- struct{}{}:
		return nil
	default:
		return fail(apperror.RateLimited)
	}
}
func (p *ArgonPasswords) Hash(ctx context.Context, password string) (string, error) {
	if !validPassword(password) {
		return "", fail(apperror.ValidationFailed)
	}
	if err := p.acquire(ctx); err != nil {
		return "", err
	}
	defer func() { <-p.slots }()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fail(apperror.ServiceUnavailable)
	}
	hash := encodeHash(password, salt)
	if ctx.Err() != nil {
		return "", fail(apperror.ServiceUnavailable)
	}
	return hash, nil
}
func (p *ArgonPasswords) Verify(ctx context.Context, password, encoded string) (bool, error) {
	if len(encoded) > 512 || len(password) > 128 || !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") && encoded != "" {
		return false, nil
	}
	missing := encoded == ""
	if missing {
		encoded = p.dummy
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false, nil
	}
	salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	key, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		return false, nil
	}
	if err := p.acquire(ctx); err != nil {
		return false, err
	}
	defer func() { <-p.slots }()
	actual := argon2.IDKey([]byte(password), salt, 3, 65536, 2, 32)
	equal := subtle.ConstantTimeCompare(actual, key) == 1
	if ctx.Err() != nil {
		return false, fail(apperror.ServiceUnavailable)
	}
	return equal && !missing, nil
}
