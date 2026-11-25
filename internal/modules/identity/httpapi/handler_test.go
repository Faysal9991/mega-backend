package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type fixtureRepo struct {
	identity.Repository
	failed bool
	rates  int
}

func (r *fixtureRepo) Access(_ context.Context, hash, install string) (identity.Principal, error) {
	if r.failed {
		return identity.Principal{}, identity.ErrStorage
	}
	if hash != identity.Digest(strings.Repeat("a", 43)) || install != strings.Repeat("b", 32) {
		return identity.Principal{}, identity.ErrMissing
	}
	return identity.Principal{SessionID: strings.Repeat("c", 32), Account: identity.Account{ID: strings.Repeat("d", 32), Status: "active", DisplayName: "Synthetic"}}, nil
}
func (r *fixtureRepo) Identifiers(context.Context, string) ([]identity.Alias, error) {
	return []identity.Alias{{Kind: "username", Value: "synthetic"}}, nil
}
func (r *fixtureRepo) Rate(context.Context, string) (int32, error) {
	r.rates++
	if r.failed {
		return 0, identity.ErrStorage
	}
	return 31, nil
}

type unusedPasswords struct{}

func (unusedPasswords) Hash(context.Context, string) (string, error) {
	panic("hash work must not be reached")
}
func (unusedPasswords) Verify(context.Context, string, string) (bool, error) {
	panic("hash work must not be reached")
}
func TestIdentityHTTPContractsAndFailClosed(t *testing.T) {
	repo := &fixtureRepo{}
	s, err := identity.New(repo, identity.NewGoogle(""), unusedPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h, err := New(s, logger)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := httpserver.NewLifecycle(nil)
	router, err := httpserver.NewRouter(cfg.HTTP, logger, lifecycle, func(r chi.Router) { h.Mount(r) })
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, path, body, authorization, installation string
		want                                                  int
	}{
		{"current", "GET", "/identity/me", "", "Bearer " + strings.Repeat("a", 43), strings.Repeat("b", 32), 200},
		{"missing_auth", "GET", "/identity/me", "", "", "", 401},
		{"wrong_installation", "GET", "/identity/me", "", "Bearer " + strings.Repeat("a", 43), strings.Repeat("e", 32), 401},
		{"strict_dto", "POST", "/auth/google/challenges", `{"installation_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","purpose":"login","staff":true}`, "", "", 400},
		{"rate_before_hash", "POST", "/auth/password/login", `{"installation_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","identifier":"synthetic","password":"synthetic password value"}`, "", "", 429},
		{"no_signup", "POST", "/auth/signup", `{}`, "", "", 404},
	}
	var successful map[string]any
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1"+tc.path, bytes.NewBufferString(tc.body))
			req.RemoteAddr = "192.0.2.1:1234"
			req.Header.Set("Content-Type", "application/json")
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			if tc.installation != "" {
				req.Header.Set("X-Installation-ID", tc.installation)
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatalf("status got %d want %d", out.Code, tc.want)
			}
			if tc.want != 404 && out.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("identity response cacheable")
			}
			if tc.want == 429 && out.Header().Get("Retry-After") != "60" {
				t.Fatal("missing rate backoff")
			}
			var body map[string]any
			if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["request_id"] == "" || out.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request correlation")
			}
			if tc.want == 200 {
				successful = body
			}
		})
	}
	raw, err := os.ReadFile("../../../../contracts/openapi/fixtures/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Schema string         `json:"schema"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	want := fixtures[0].Body
	delete(successful, "request_id")
	delete(want, "request_id")
	a, _ := json.Marshal(successful)
	b, _ := json.Marshal(want)
	if !bytes.Equal(a, b) {
		t.Fatal("current-identity wire fixture drift")
	}
	repo.failed = true
	req := httptest.NewRequest("GET", "/api/v1/identity/me", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
	req.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != 503 {
		t.Fatal("PostgreSQL security failure permitted access")
	}
	// No Redis dependency is injected or consulted in any authentication path.
	if repo.rates != 1 {
		t.Fatal("abuse check did not precede expensive work")
	}
}
