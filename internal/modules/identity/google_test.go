package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoogleSignedClaimsAndCachedKeys(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "public,max-age=3600")
		_ = json.NewEncoder(w).Encode(keys)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	g := &Google{clientID: "fixture", parties: []string{"fixture"}, verifier: oidc.NewVerifier("https://accounts.google.com", oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), server.URL), &oidc.Config{ClientID: "fixture", SupportedSigningAlgs: []string{"RS256"}})}
	nonce := Digest("synthetic-challenge")
	now := time.Now()
	sign := func(k *rsa.PrivateKey, change func(map[string]any)) string {
		t.Helper()
		claims := map[string]any{"iss": "https://accounts.google.com", "aud": "fixture", "sub": "stable-subject", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": nonce, "name": "Synthetic", "email": "synthetic@example.invalid", "picture": ""}
		if change != nil {
			change(claims)
		}
		raw, err := json.Marshal(claims)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: k}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "fixture"))
		if err != nil {
			t.Fatal(err)
		}
		signed, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		token, err := signed.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token := sign(key, nil)
	got, err := g.Verify(context.Background(), token, nonce, now)
	if err != nil || got.Subject != "stable-subject" {
		t.Fatal("valid signed identity rejected")
	}
	before := requests.Load()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := g.Verify(context.Background(), token, nonce, now); err != nil {
				t.Error("cached concurrent verification failed")
			}
		})
	}
	wg.Wait()
	if requests.Load() != before {
		t.Fatal("signing keys were not cached")
	}
	cases := []struct {
		name   string
		k      *rsa.PrivateKey
		change func(map[string]any)
	}{
		{"signature", other, nil},
		{"issuer", key, func(c map[string]any) { c["iss"] = "https://attacker.invalid" }},
		{"audience", key, func(c map[string]any) { c["aud"] = "other-app" }},
		{"expiry", key, func(c map[string]any) { c["exp"] = now.Add(-time.Hour).Unix() }},
		{"nonce", key, func(c map[string]any) { c["nonce"] = "different-challenge" }},
		{"old_issued", key, func(c map[string]any) { c["iat"] = now.Add(-10 * time.Minute).Unix() }},
		{"future_issued", key, func(c map[string]any) { c["iat"] = now.Add(time.Hour).Unix() }},
		{"missing_issued", key, func(c map[string]any) { delete(c, "iat") }},
		{"subject", key, func(c map[string]any) { c["sub"] = "" }},
		{"authorized_party", key, func(c map[string]any) { c["azp"] = "attacker" }},
		{"multiple_audiences", key, func(c map[string]any) { c["aud"] = []string{"fixture", "other"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := g.Verify(context.Background(), sign(tc.k, tc.change), nonce, now); apperror.Public(err).Code() != apperror.Unauthenticated || err.Error() != "UNAUTHENTICATED" {
				t.Fatal("unsafe invalid-token result", err)
			}
		})
	}
	if _, err := NewGoogle("").Verify(context.Background(), token, nonce, now); apperror.Public(err).Code() != apperror.ServiceUnavailable {
		t.Fatal("missing Google configuration enabled verifier")
	}
}
