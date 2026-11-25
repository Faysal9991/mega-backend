package identity

import (
	"context"
	"encoding/json"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"os"
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

func TestPasswordUTF8ByteContract(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/fixtures/password_utf8.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Bytes int    `json:"utf8_bytes"`
		Valid bool   `json:"valid_new_password"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	passwords := NewPasswords()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if len(tc.Value) != tc.Bytes || validPassword(tc.Value) != tc.Valid {
				t.Fatal("runtime UTF-8 byte policy disagrees with shared contract fixture")
			}
			hash, err := passwords.Hash(context.Background(), tc.Value)
			if !tc.Valid {
				if apperror.Public(err).Code() != apperror.ValidationFailed {
					t.Fatal("invalid byte length accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := passwords.Verify(context.Background(), tc.Value, hash); err != nil || !ok {
				t.Fatal("Unicode password did not roundtrip")
			}
		})
	}
}
