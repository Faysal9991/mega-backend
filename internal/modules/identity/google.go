package identity

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/coreos/go-oidc/v3/oidc"
	"net/http"
	"slices"
	"time"
)

type Google struct {
	verifier *oidc.IDTokenVerifier
	clientID string
	parties  []string
}

func NewGoogle(clientID string, parties ...string) *Google {
	g := &Google{clientID: clientID, parties: append([]string{clientID}, parties...)}
	if clientID == "" {
		return g
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxConnsPerHost = 4
	transport.MaxIdleConnsPerHost = 2
	transport.ResponseHeaderTimeout = 3 * time.Second
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx := oidc.ClientContext(context.Background(), client)
	g.verifier = oidc.NewVerifier("https://accounts.google.com", oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs"), &oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}})
	return g
}
func (g *Google) Verify(ctx context.Context, raw, nonce string, created time.Time) (GoogleIdentity, error) {
	if g.verifier == nil {
		return GoogleIdentity{}, fail(apperror.ServiceUnavailable)
	}
	if len(raw) == 0 || len(raw) > 16384 {
		return GoogleIdentity{}, fail(apperror.Unauthenticated)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, err := g.verifier.Verify(bounded, raw)
	if err != nil {
		return GoogleIdentity{}, fail(apperror.Unauthenticated)
	}
	var claims struct {
		Nonce           string `json:"nonce"`
		Issued          int64  `json:"iat"`
		AuthTime        int64  `json:"auth_time"`
		AuthorizedParty string `json:"azp"`
		Name            string `json:"name"`
		Email           string `json:"email"`
		Picture         string `json:"picture"`
	}
	now := time.Now()
	if token.Claims(&claims) != nil || len(token.Subject) < 1 || len(token.Subject) > 255 || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 || claims.Issued == 0 || time.Unix(claims.Issued, 0).Before(now.Add(-5*time.Minute)) || time.Unix(claims.Issued, 0).Before(created.Add(-30*time.Second)) || time.Unix(claims.Issued, 0).After(now.Add(30*time.Second)) || claims.AuthTime > now.Add(30*time.Second).Unix() || claims.AuthorizedParty != "" && !slices.Contains(g.parties, claims.AuthorizedParty) || len(token.Audience) > 1 && claims.AuthorizedParty == "" {
		return GoogleIdentity{}, fail(apperror.Unauthenticated)
	}
	if len(claims.Name) > 256 || len(claims.Email) > 320 || len(claims.Picture) > 2048 {
		return GoogleIdentity{}, fail(apperror.Unauthenticated)
	}
	return GoogleIdentity{Subject: token.Subject, DisplayName: claims.Name, Email: claims.Email, Avatar: claims.Picture}, nil
}
