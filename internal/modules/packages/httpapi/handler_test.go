package httpapi

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stub struct {
	calls int
	err   error
}

func (s *stub) Execute(_ context.Context, _ access.Credentials, in packages.Command) (any, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if in.Definition == nil {
		return nil, invalid()
	}
	return *in.Definition, nil
}
func (s *stub) Read(_ context.Context, _ access.Credentials, q packages.Query) (packages.Page, error) {
	s.calls++
	return packages.Page{Items: []packages.Definition{}, Limit: q.Limit}, s.err
}
func (s *stub) Purchase(context.Context, access.Credentials, string, string) (packages.Purchase, error) {
	s.calls++
	return packages.Purchase{FinancialStatus: "committed"}, s.err
}
func (s *stub) Powers(context.Context, access.Credentials, string) (access.Effective, error) {
	s.calls++
	return access.Effective{Permissions: []string{"room.enter"}}, s.err
}
func (s *stub) CapabilityPolicy(context.Context, access.Credentials, *access.PolicyChange) (any, error) {
	s.calls++
	return struct{}{}, s.err
}
func (s *stub) Override(context.Context, access.Credentials, string, access.OverrideChange) error {
	s.calls++
	return s.err
}
func (s *stub) CreateModeratorRole(context.Context, access.Credentials, string, string, string) (access.Role, error) {
	s.calls++
	return access.Role{}, s.err
}

type tracked struct {
	http.ResponseWriter
	status int
}

func (w *tracked) Status() int            { return w.status }
func (w *tracked) WriteHeader(status int) { w.status = status; w.ResponseWriter.WriteHeader(status) }
func (w *tracked) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func testHandler(t *testing.T, s *stub) http.Handler {
	t.Helper()
	h, e := New(s, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.ServeHTTP(&tracked{ResponseWriter: w}, q.WithContext(requestmeta.WithID(q.Context(), "capability-test")))
	})
}
func TestStrictPackageAndCapabilityBoundaries(t *testing.T) {
	for _, v := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/packages?unknown=x&limit=5", "", 422}, {"GET", "/packages?limit=1&limit=2", "", 422}, {"POST", "/packages/" + strings.Repeat("a", 32) + "/purchases", `{"diamond_price":1}`, 400}, {"POST", "/packages/" + strings.Repeat("a", 32) + "/purchases", `{"actor_id":"x"}`, 400}, {"PUT", "/admin/users/" + strings.Repeat("a", 32) + "/capabilities", `{"action":"room.enter","reason":"x"}`, 422}, {"PUT", "/admin/users/" + strings.Repeat("a", 32) + "/capabilities", `{"action":"room.enter","allowed":null,"reason":"x"}`, 400}, {"POST", "/admin/packages", `{"definition":{"Active":true},"reason":"x"}`, 400}, {"POST", "/admin/packages", `{"definition":{"active":null},"reason":"x"}`, 400}, {"POST", "/admin/packages", `{"definition":{"active":true,"live_listing_priority":null},"reason":"x"}`, 400}} {
		t.Run(v.path+v.body, func(t *testing.T) {
			s := &stub{}
			h := testHandler(t, s)
			r := httptest.NewRequest(v.method, v.path, strings.NewReader(v.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "synthetic-key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != v.status || s.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, s.calls, w.Body.String())
			}
		})
	}
}
func TestCentralSafeEnvelopesAndBoundedLists(t *testing.T) {
	for _, code := range []apperror.Code{"", apperror.Unauthenticated, apperror.Forbidden, apperror.Conflict, apperror.ServiceUnavailable} {
		s := &stub{}
		if code != "" {
			s.err = apperror.New(code, context.DeadlineExceeded)
		}
		h := testHandler(t, s)
		r := httptest.NewRequest("GET", "/packages?limit=2", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		body := w.Body.String()
		if !strings.Contains(body, `"request_id":"capability-test"`) || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(body, "deadline") {
			t.Fatal(body)
		}
		if code == "" && (!strings.Contains(body, `"data":[]`) || !strings.Contains(body, `"limit":2`)) {
			t.Fatal(body)
		}
		if code != "" && !strings.Contains(body, string(code)) {
			t.Fatal(body)
		}
	}
}
