package identity

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"strings"
	"testing"
)

func TestPasswordPolicyAndHashLimits(t *testing.T) {
	p := NewPasswords()
	ctx := context.Background()
	password := "synthetic password value"
	first, err := p.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Hash(ctx, password)
	if err != nil || first == second {
		t.Fatal("salt reuse")
	}
	if ok, err := p.Verify(ctx, password, first); err != nil || !ok {
		t.Fatal("correct password rejected")
	}
	for _, hash := range []string{first, ""} {
		if ok, err := p.Verify(ctx, "wrong password", hash); err != nil || ok {
			t.Fatal("wrong/missing password accepted")
		}
	}
	for _, raw := range []string{"short", strings.Repeat("x", 129), string([]byte{255})} {
		if _, err := p.Hash(ctx, raw); apperror.Public(err).Code() != apperror.ValidationFailed {
			t.Fatal("password limits bypassed")
		}
	}
	for _, hash := range []string{"$argon2id$v=19$m=999999999,t=999,p=999$invalid$invalid", strings.Repeat("x", 600), "$argon2id$v=19$m=65536,t=3,p=2$bad$bad"} {
		if ok, err := p.Verify(ctx, password, hash); ok || err != nil {
			t.Fatal("unbounded/invalid PHC accepted")
		}
	}
	p.slots <- struct{}{}
	p.slots <- struct{}{}
	if _, err := p.Hash(ctx, password); apperror.Public(err).Code() != apperror.RateLimited {
		t.Fatal("hash concurrency not bounded")
	}
	if _, err := p.Verify(ctx, password, first); apperror.Public(err).Code() != apperror.RateLimited {
		t.Fatal("verify concurrency not bounded")
	}
	<-p.slots
	<-p.slots
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Hash(canceled, password); err == nil {
		t.Fatal("canceled hashing admitted")
	}
}
func TestIdentifierNormalization(t *testing.T) {
	for _, tc := range []struct{ raw, kind, want string }{{" Example_1 ", "username", "example_1"}, {"+8801712345678", "phone", "+8801712345678"}} {
		kind, value, err := NormalizeIdentifier(tc.raw)
		if err != nil || kind != tc.kind || value != tc.want {
			t.Fatal("normalization mismatch")
		}
	}
	for _, bad := range []string{"a@b.com", "aa", "123name", "নাম", "Kelvin", "01712345678", "+0123456789", "+880 1712345678", " +8801712345678", strings.Repeat("a", 33)} {
		if _, _, err := NormalizeIdentifier(bad); err == nil {
			t.Fatal("ambiguous identifier accepted")
		}
	}
}
