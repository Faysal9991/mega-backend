package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
)

func checkErrorContract(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var got response.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Success || string(got.Code) != code || got.Message == "" || got.RequestID != rec.Header().Get("X-Request-ID") || got.RequestID == "" || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("invalid middleware error: %s", rec.Body)
	}
}
func TestMiddlewareErrorContracts(t *testing.T) {
	var out bytes.Buffer
	router := testRouter(t, map[string]string{"HTTP_MAX_BODY_BYTES": "1024", "HTTP_REQUEST_TIMEOUT": "1ms"}, &out, func(api chi.Router) {
		api.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("PRIVATE_SENTINEL") })
		api.Get("/deadline", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		api.Get("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	for _, tt := range []struct {
		method, path, code string
		status             int
	}{
		{"GET", "/missing", "NOT_FOUND", 404}, {"POST", "/api/v1/ok", "METHOD_NOT_ALLOWED", 405}, {"POST", "/api/v1/ok", "PAYLOAD_TOO_LARGE", 413}, {"GET", "/api/v1/panic", "INTERNAL_ERROR", 500}, {"GET", "/api/v1/deadline", "REQUEST_TIMEOUT", 504},
	} {
		var body io.Reader
		if tt.status == 413 {
			body = strings.NewReader(strings.Repeat("x", 1025))
		}
		r := httptest.NewRequest(tt.method, tt.path, body)
		r.Header.Set("X-Request-ID", "contract-id")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != tt.status {
			t.Fatalf("got %d; want %d", rec.Code, tt.status)
		}
		checkErrorContract(t, rec, tt.code)
		if tt.status == 405 && rec.Header().Get("Allow") != "GET" {
			t.Fatal("Allow lost")
		}
	}
	// HEAD infrastructure errors retain metadata, suppress bodies, and log zero bytes.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("HEAD", "/missing", nil))
	if rec.Code != 404 || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal("HEAD middleware error")
	}
	logs := records(t, out.String())
	last := logs[len(logs)-1]
	if last["response_bytes"] != float64(0) || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe completion")
	}
}
func TestDecoderMiddlewareIntegration(t *testing.T) {
	decoder, _ := validation.NewDecoder(2048)
	responses, _ := response.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	router := testRouter(t, map[string]string{"HTTP_MAX_BODY_BYTES": "1024"}, io.Discard, func(api chi.Router) {
		api.Post("/decode", func(w http.ResponseWriter, r *http.Request) {
			var dto struct {
				Name string `json:"name"`
			}
			if err := decoder.Decode(r, &dto); err != nil {
				if err := responses.Error(w, r, err); err != nil {
					t.Error(err)
				}
				return
			}
			if err := responses.Success(w, r, 201, "TEST_CREATED", response.List[string](nil), response.Options{}); err != nil {
				t.Error(err)
			}
		})
	})
	for _, tt := range []struct {
		body, media, code string
		status            int
	}{
		{`{"name":"ok"}`, "application/json", "", 201},
		{`{"extra":1}`, "application/json", "INVALID_JSON", 400},
		{`{"name":"ok"}`, "text/plain", "UNSUPPORTED_MEDIA_TYPE", 415},
		{`{"name":"` + strings.Repeat("x", 1024) + `"}`, "application/json", "PAYLOAD_TOO_LARGE", 413},
	} {
		for _, chunked := range []bool{false, true} {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/v1/decode", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.media)
			if chunked {
				r.ContentLength = -1
				r.TransferEncoding = []string{"chunked"}
			}
			router.ServeHTTP(rec, r)
			if rec.Code != tt.status {
				t.Fatalf("decoder status %d; want %d", rec.Code, tt.status)
			}
			if tt.code != "" {
				checkErrorContract(t, rec, tt.code)
			} else if !strings.Contains(rec.Body.String(), `"data":[]`) {
				t.Fatal("nil list contract")
			}
		}
	}
}

type informationalWriter struct {
	header   http.Header
	statuses []int
	body     bytes.Buffer
}

func (w *informationalWriter) Header() http.Header         { return w.header }
func (w *informationalWriter) WriteHeader(code int)        { w.statuses = append(w.statuses, code) }
func (w *informationalWriter) Write(p []byte) (int, error) { return w.body.Write(p) }
func TestInformationalResponseBeforeEnvelope(t *testing.T) {
	responses, _ := response.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := testRouter(t, nil, io.Discard, func(api chi.Router) {
		api.Get("/early", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(103)
			if err := responses.Success(w, r, 202, "TEST_ACCEPTED", struct{}{}, response.Options{}); err != nil {
				t.Error(err)
			}
		})
		api.Get("/panic", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(103); panic("PRIVATE_SENTINEL") })
	})
	for _, path := range []string{"early", "panic"} {
		w := &informationalWriter{header: make(http.Header)}
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+path, nil))
		want := 202
		if path == "panic" {
			want = 500
		}
		if len(w.statuses) != 2 || w.statuses[0] != 103 || w.statuses[1] != want || !json.Valid(w.body.Bytes()) || strings.Contains(w.body.String(), "PRIVATE_SENTINEL") {
			t.Fatal("informational response finalized early")
		}
	}
}
func TestFlushAndAbortNeverAppend(t *testing.T) {
	for _, mode := range []string{"flush", "204", "abort before", "abort after"} {
		var out bytes.Buffer
		router := testRouter(t, nil, &out, func(api chi.Router) {
			api.Get("/test", func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "flush":
					w.(http.Flusher).Flush()
				case "204":
					w.WriteHeader(204)
				case "abort before":
					panic(http.ErrAbortHandler)
				case "abort after":
					_, _ = w.Write([]byte("partial"))
					panic(http.ErrAbortHandler)
				}
				panic("PRIVATE_SENTINEL")
			})
		})
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				if recover() != http.ErrAbortHandler {
					t.Error("abort sentinel lost")
				}
			}()
			router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/test", nil))
		}()
		want := ""
		if mode == "abort after" {
			want = "partial"
		}
		if rec.Body.String() != want || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
			t.Fatal("response appended or leaked")
		}
		if mode == "204" && rec.Code != 204 {
			t.Fatal("204 replaced")
		}
		if strings.HasPrefix(mode, "abort") && strings.Contains(out.String(), "http.handler_panicked") {
			t.Fatal("explicit abort treated as panic")
		}
	}
}

type panicJSON struct{}

func (panicJSON) MarshalJSON() ([]byte, error) { panic("PRIVATE_SENTINEL") }
func TestEncodingPanicAndErrorIntegrated(t *testing.T) {
	for _, data := range []any{panicJSON{}, make(chan int)} {
		var out bytes.Buffer
		responses, _ := response.New(slog.New(slog.NewJSONHandler(&out, nil)))
		router := testRouter(t, nil, &out, func(api chi.Router) {
			api.Get("/test", func(w http.ResponseWriter, r *http.Request) {
				if err := responses.Success(w, r, 201, "TEST_CREATED", data, response.Options{}); err != nil {
					t.Error(err)
				}
			})
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/test", nil))
		checkErrorContract(t, rec, "INTERNAL_ERROR")
		if rec.Code != 500 || strings.Contains(out.String()+rec.Body.String(), "PRIVATE_SENTINEL") {
			t.Fatal("unsafe encoding failure")
		}
	}
}

type brokenResponseWriter struct {
	header          http.Header
	headers, writes int
}

func (w *brokenResponseWriter) Header() http.Header { return w.header }
func (w *brokenResponseWriter) WriteHeader(int)     { w.headers++ }
func (w *brokenResponseWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("PRIVATE_SENTINEL")
}
func TestInfrastructureWriteFailure(t *testing.T) {
	var out bytes.Buffer
	router := testRouter(t, nil, &out, nil)
	w := &brokenResponseWriter{header: make(http.Header)}
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Error("failed write must abort")
			}
		}()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	}()
	if w.headers != 1 || w.writes != 1 || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("second response or raw error")
	}
	logs := records(t, out.String())
	if logs[len(logs)-1]["status"] != float64(404) || logs[len(logs)-1]["response_bytes"] != float64(0) {
		t.Fatal("failed write accounting")
	}
}

// Verify 503 JSON without changing the synchronization/capacity regression test.
func TestAdmissionErrorContract(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := testRouter(t, map[string]string{"HTTP_MAX_CONCURRENT_REQUESTS": "1"}, io.Discard, func(api chi.Router) {
		api.Get("/block", func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) })
	})
	go func() {
		defer close(done)
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/block", nil))
	}()
	await(t, entered)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	close(release)
	await(t, done)
	checkErrorContract(t, rec, string(apperror.ServiceUnavailable))
	if rec.Code != 503 || rec.Header().Get("Retry-After") != "1" {
		t.Fatal("overload contract")
	}
}

func TestCommittedResponseSurvivesDeadline(t *testing.T) {
	router := testRouter(t, map[string]string{"HTTP_REQUEST_TIMEOUT": "1ms"}, io.Discard, func(api chi.Router) {
		api.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("partial"))
			<-r.Context().Done()
		})
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/test", nil))
	if rec.Code != 200 || rec.Body.String() != "partial" {
		t.Fatal("timeout appended to committed response")
	}
}
