package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/go-chi/chi/v5"
)

type serviceStub struct {
	Service
	calls    int
	previous string
}

func (s *serviceStub) Preferences(context.Context, access.Credentials, *delivery.Preferences) (delivery.Preferences, error) {
	s.calls++
	return delivery.Preferences{}, nil
}
func (s *serviceStub) Gifts(context.Context, access.Credentials, string, string, int, bool) (delivery.Page, error) {
	s.calls++
	return delivery.Page{Data: []delivery.Gift{}}, nil
}

type walletStub struct{ calls int }

func (s *walletStub) Execute(context.Context, access.Credentials, wallet.Command) (any, error) {
	s.calls++
	return map[string]string{"operation_id": strings.Repeat("a", 32)}, nil
}
func TestStrictDeliveryRequests(t *testing.T) {
	service := &serviceStub{}
	wallet := &walletStub{}
	h, e := New(service, wallet, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(&testTracked{ResponseWriter: w}, r) })
	})
	h.Mount(r)
	for _, tc := range []struct{ method, path, body string }{{"POST", "/rooms/" + strings.Repeat("a", 32) + "/gifts", `{"session_id":"` + strings.Repeat("b", 32) + `","recipient_id":"` + strings.Repeat("c", 32) + `","gift_id":"` + strings.Repeat("d", 32) + `","quantity":1,"price":1}`}, {"PUT", "/notification-preferences", `{"gift_received":true}`}, {"PUT", "/notification-preferences", `{"gift_received":null,"agency_decision":false,"account_event":false,"room_invitation":false,"admin_announcement":false}`}, {"POST", "/admin/gifts", `{"code":"rose","diamond_price":1,"active":true,"display_order":0,"previous_version_id":null}`}, {"GET", "/gifts?actor_id=bad&limit=20", ""}, {"GET", "/gifts?limit=51", ""}, {"GET", "/gifts?limit=", ""}, {"PUT", "/notification-preferences", `{"Gift_received":true,"agency_decision":false,"account_event":false,"room_invitation":false,"admin_announcement":false}`}} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req = req.WithContext(requestmeta.WithID(req.Context(), "delivery-test"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatal("invalid request accepted", tc.path, w.Code)
		}
	}
	if service.calls != 0 || wallet.calls != 0 {
		t.Fatal("invalid data reached services")
	}
	req := httptest.NewRequest("GET", "/gifts", nil)
	req = req.WithContext(requestmeta.WithID(req.Context(), "delivery-test"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatal(w.Body.String())
	}
}
func TestAmbiguousCredentialsRejected(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Authorization", "Bearer first")
	r.Header.Add("Authorization", "Bearer second")
	r.Header.Set("X-Installation-ID", "one")
	if Credentials(r).Access != "" {
		t.Fatal("ambiguous bearer accepted")
	}
}

type testTracked struct {
	http.ResponseWriter
	status int
}

func (w *testTracked) Status() int { return w.status }
func (w *testTracked) WriteHeader(s int) {
	if w.status == 0 {
		w.status = s
		w.ResponseWriter.WriteHeader(s)
	}
}
func (w *testTracked) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}

func TestRuntimeResponseContractFixtures(t *testing.T) {
	data, e := os.ReadFile("../../../../contracts/openapi/fixtures/delivery_responses.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Schema string         `json:"schema"`
		Body   map[string]any `json:"body"`
	}
	if e = json.Unmarshal(data, &fixtures); e != nil {
		t.Fatal(e)
	}
	s := &serviceStub{}
	h, e := New(s, &walletStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	router := chi.NewRouter()
	h.Mount(router)
	for _, fixture := range fixtures {
		path := "/gifts"
		if fixture.Schema == "preferences_getEnvelope" {
			path = "/notification-preferences"
		}
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(requestmeta.WithID(req.Context(), "delivery-test"))
		raw := httptest.NewRecorder()
		router.ServeHTTP(&testTracked{ResponseWriter: raw}, req)
		var actual map[string]any
		if e = json.Unmarshal(raw.Body.Bytes(), &actual); e != nil || !reflect.DeepEqual(actual, fixture.Body) {
			t.Fatalf("runtime response differs from schema fixture %s", fixture.Schema)
		}
	}
}

func (s *serviceStub) RegisterWithPrevious(_ context.Context, _ access.Credentials, _, _, previous string) error {
	s.calls++
	s.previous = previous
	return nil
}
func TestExplicitSwitchProofIsNotReturned(t *testing.T) {
	s := &serviceStub{}
	h, e := New(s, &walletStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	router := chi.NewRouter()
	h.Mount(router)
	proof := strings.Repeat("p", 43)
	token := "synthetic-device-token"
	body := `{"platform":"android","token":"` + token + `","previous_access_token":"` + proof + `"}`
	req := httptest.NewRequest("PUT", "/push/device", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(requestmeta.WithID(req.Context(), "delivery-test"))
	raw := httptest.NewRecorder()
	router.ServeHTTP(&testTracked{ResponseWriter: raw}, req)
	if raw.Code != 200 || s.previous != proof || strings.Contains(raw.Body.String(), proof) || strings.Contains(raw.Body.String(), token) {
		t.Fatal("switch proof was dropped or exposed")
	}
}
