package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	result    *live.Result
	tokenData any
	Service
	called int
	last   live.Command
	err    error
}

func (s *fakeService) Execute(_ context.Context, _ access.Credentials, c live.Command) (live.Result, error) {
	s.called++
	s.last = c
	if s.result != nil {
		return *s.result, s.err
	}
	return live.Result{Data: struct{}{}}, s.err
}
func (s *fakeService) Read(context.Context, access.Credentials, live.Query) (live.Page, error) {
	s.called++
	return live.Page{Data: []any{}}, s.err
}
func (s *fakeService) Token(context.Context, access.Credentials, string) (any, error) {
	s.called++
	if s.tokenData != nil {
		return s.tokenData, s.err
	}
	return struct{}{}, s.err
}
func (s *fakeService) Reconcile(context.Context, access.Credentials, string) error {
	s.called++
	return s.err
}
func (s *fakeService) Ingest(context.Context, live.Event) error { s.called++; return s.err }
func setup(t *testing.T, s *fakeService, v Verifier) http.Handler {
	t.Helper()
	if v == nil {
		v = func(*http.Request) (live.Event, error) { return live.Event{}, errors.New("invalid signature") }
	}
	h, e := New(s, v, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&tracked{ResponseWriter: w}, r.WithContext(requestmeta.WithID(r.Context(), "fixture-request")))
		})
	})
	h.Mount(r)
	return r
}
func TestStrictMutationDTOs(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, v := range []struct{ method, path, body, kind string }{{"POST", "/agencies", `{"name":"Agency"}`, "agency.create"}, {"PATCH", "/admin/agencies/" + id, `{"name":"Agency","status":"active","reason":"review"}`, "agency.update"}, {"POST", "/agencies/" + id + "/host-applications", `{"classification":"audio_host"}`, "host.apply"}, {"POST", "/host-applications/" + id + "/decision", `{"state":"approved","reason":"review"}`, "host.decide"}, {"POST", "/rooms", `{"title":"Room","room_type":"audio","capacity":10,"seats":5}`, "room.create"}, {"POST", "/rooms/" + id + "/join", `{}`, "room.join"}, {"POST", "/rooms/" + id + "/seats/accept", `{}`, "seat.accept"}, {"PATCH", "/rooms/" + id + "/members/" + id + "/mute", `{"muted":true,"reason":"review"}`, "member.mute"}, {"POST", "/admin/rooms/" + id + "/close", `{}`, "room.close"}} {
		t.Run(v.kind, func(t *testing.T) {
			s := &fakeService{}
			h := setup(t, s, nil)
			r := httptest.NewRequest(v.method, v.path, strings.NewReader(v.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "command-key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || s.called != 1 || s.last.Kind != v.kind {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, body := range []string{strings.TrimSuffix(v.body, "}") + `,"actor_id":"injected"}`, `{"name":"one","Name":"two"}`, strings.Repeat(" ", 4097)} {
				s.called = 0
				r := httptest.NewRequest(v.method, v.path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code < 400 || s.called != 0 {
					t.Fatal("strict DTO bypass", w.Code)
				}
			}
		})
	}
}
func TestAdminFailuresAndPagination(t *testing.T) {
	for _, code := range []apperror.Code{apperror.Unauthenticated, apperror.Forbidden, apperror.Conflict, apperror.ServiceUnavailable} {
		s := &fakeService{err: live.Fail(code)}
		h := setup(t, s, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/rooms", nil))
		if w.Code < 400 || !strings.Contains(w.Body.String(), string(code)) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	s := &fakeService{}
	h := setup(t, s, nil)
	for _, q := range []string{"?limit=0", "?limit=51", "?limit=1&limit=2", "?actor_id=x"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/rooms"+q, nil))
		if w.Code != 422 {
			t.Fatal(q, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/rooms", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":[]`) || !strings.Contains(w.Body.String(), `"pagination"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestWebhookBodyAndSignatureBoundaries(t *testing.T) {
	s := &fakeService{}
	h := setup(t, s, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/providers/livekit/webhook", strings.NewReader(`{}`)))
	if w.Code != 401 || s.called != 0 {
		t.Fatal(w.Code)
	}
	h = setup(t, s, func(r *http.Request) (live.Event, error) { _, e := io.ReadAll(r.Body); return live.Event{}, e })
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/providers/livekit/webhook", strings.NewReader(strings.Repeat("x", (64<<10)+1)))
	r.ContentLength = -1
	h.ServeHTTP(w, r)
	if w.Code != 413 || s.called != 0 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/providers/livekit/webhook", strings.NewReader(`{}`)))
	if w.Code != 200 || s.called != 1 {
		t.Fatal(w.Code)
	}
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

func TestRuntimeResponseContractFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../../contracts/openapi/fixtures/live.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Schema string         `json:"schema"`
		Body   map[string]any `json:"body"`
	}
	if e := json.Unmarshal(raw, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, f := range fixtures {
		if f.Schema != "room_createEnvelope" && f.Schema != "TokenEnvelope" {
			continue
		}
		s := &fakeService{}
		path := "/rooms"
		body := `{"title":"Synthetic room","room_type":"audio","capacity":20,"seats":5}`
		status := 202
		if f.Schema == "room_createEnvelope" {
			b, _ := json.Marshal(f.Body["data"])
			var result live.Result
			if e := json.Unmarshal(b, &result); e != nil {
				t.Fatal(e)
			}
			s.result = &result
		} else {
			s.tokenData = f.Body["data"]
			path = "/rooms/" + strings.Repeat("a", 32) + "/token"
			body = `{}`
			status = 200
		}
		h := setup(t, s, nil)
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "fixture-command")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(f.Schema, w.Code, w.Body.String())
		}
		var got map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		f.Body["request_id"] = "fixture-request"
		if !reflect.DeepEqual(got, f.Body) {
			t.Fatalf("runtime/contract response mismatch: %s", f.Schema)
		}
	}
}

func TestRoomCreateRequiresExplicitSeatCount(t *testing.T) {
	for _, body := range []string{`{"title":"Room","room_type":"video","capacity":10}`, `{"title":"Room","room_type":"video","capacity":10,"seats":null}`} {
		s := &fakeService{}
		h := setup(t, s, nil)
		r := httptest.NewRequest("POST", "/rooms", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 422 || s.called != 0 {
			t.Fatal("implicit/null seat count reached application")
		}
	}
}
