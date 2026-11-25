package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
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
	cfg, err := config.Load(func(key string) (string, bool) {
		if key == "LOG_FORMAT" {
			return "json", true
		}
		return "", false
	})
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

type failingIdentityWriter struct {
	header                         http.Header
	status, headers, writes, bytes int
	short                          bool
}

func (w *failingIdentityWriter) Header() http.Header    { return w.header }
func (w *failingIdentityWriter) WriteHeader(status int) { w.status = status; w.headers++ }
func (w *failingIdentityWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.short {
		w.bytes = len(p) - 1
		return w.bytes, nil
	}
	return 0, errors.New("PRIVATE_WRITE_ERROR")
}
func TestIdentityResponseWriteFailureAborts(t *testing.T) {
	for _, authorized := range []bool{true, false} {
		for _, short := range []bool{false, true} {
			t.Run(fmt.Sprintf("authorized=%t/short=%t", authorized, short), func(t *testing.T) {
				repo := &fixtureRepo{}
				service, err := identity.New(repo, identity.NewGoogle(""), unusedPasswords{})
				if err != nil {
					t.Fatal(err)
				}
				var logs bytes.Buffer
				cfg, err := config.Load(func(key string) (string, bool) {
					if key == "LOG_FORMAT" {
						return "json", true
					}
					return "", false
				})
				if err != nil {
					t.Fatal(err)
				}
				// Use production logging and the real request/commitment/recovery middleware.
				logger, err := logging.New(cfg, &logs)
				if err != nil {
					t.Fatal(err)
				}
				handler, err := New(service, logger)
				if err != nil {
					t.Fatal(err)
				}
				router, err := httpserver.NewRouter(cfg.HTTP, logger, httpserver.NewLifecycle(nil), handler.Mount)
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest("GET", "/api/v1/identity/me", nil)
				if authorized {
					request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
					request.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
				}
				writer := &failingIdentityWriter{header: make(http.Header), short: short}
				func() {
					defer func() {
						if recover() != http.ErrAbortHandler {
							t.Error("write failure did not preserve the abort sentinel")
						}
					}()
					router.ServeHTTP(writer, request)
				}()
				wantStatus := http.StatusUnauthorized
				if authorized {
					wantStatus = http.StatusOK
				}
				if writer.headers != 1 || writer.writes != 1 || writer.status != wantStatus {
					t.Fatal("committed response replaced or written twice")
				}
				if short && writer.bytes <= 0 || !short && writer.bytes != 0 {
					t.Fatal("unexpected accepted byte count")
				}
				count := 0
				for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
					var entry map[string]any
					if err := json.Unmarshal([]byte(line), &entry); err != nil {
						t.Fatal(err)
					}
					if entry["event"] == "http.handler_panicked" {
						t.Fatal("abort classified as application panic")
					}
					if entry["event"] != "http.request_completed" {
						continue
					}
					count++
					if entry["outcome"] != "aborted" || entry["status"] != float64(wantStatus) || entry["response_bytes"] != float64(writer.bytes) {
						t.Fatal("wrong completion accounting", entry)
					}
				}
				if count != 1 || strings.Contains(logs.String(), "PRIVATE_WRITE_ERROR") {
					t.Fatal("duplicate completion or raw write error exposed")
				}
			})
		}
	}
}
