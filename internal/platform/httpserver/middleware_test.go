package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/go-chi/chi/v5"
)

func testConfig(t *testing.T, values map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(func(key string) (string, bool) { value, exists := values[key]; return value, exists })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func testRouter(t *testing.T, values map[string]string, out io.Writer, register func(chi.Router)) http.Handler {
	t.Helper()
	if values == nil {
		values = map[string]string{}
	}
	values["LOG_FORMAT"] = "json"
	cfg := testConfig(t, values)
	logger, err := logging.New(cfg, out)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(cfg.HTTP, logger, &Lifecycle{}, register)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func records(t *testing.T, out string) []map[string]any {
	t.Helper()
	var result []map[string]any
	decoder := json.NewDecoder(strings.NewReader(out))
	for {
		var record map[string]any
		err := decoder.Decode(&record)
		if err == io.EOF {
			return result
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, record)
	}
}

func TestRouterAndLogging(t *testing.T) {
	var out bytes.Buffer
	router := testRouter(t, nil, &out, func(r chi.Router) {
		r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); _, _ = io.WriteString(w, "ok") })
		r.Post("/users/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/users/PRIVATE_SENTINEL?secret=PRIVATE_SENTINEL", 201},
		{"DELETE", "/api/v1/users/private", 405}, {"GET", "/unknown/PRIVATE_SENTINEL", 404},
		{"PRIVATE_SENTINEL", "/api/v1/users/hidden", 405},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tt.method, tt.path, nil)
		r.Header.Set("Authorization", "PRIVATE_SENTINEL")
		r.Header.Set("Cookie", "PRIVATE_SENTINEL")
		router.ServeHTTP(w, r)
		if w.Code != tt.status || w.Header().Get("X-Request-ID") == "" {
			t.Fatalf("status/ID = %d/%s", w.Code, w.Header().Get("X-Request-ID"))
		}
		if tt.status == 405 && w.Header().Get("Allow") != "GET, POST" {
			t.Fatalf("Allow = %q", w.Header().Get("Allow"))
		}
	}
	logs := records(t, out.String())
	if len(logs) != 4 || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("missing completion or sensitive output")
	}
	if logs[0]["route"] != "/api/v1/users/{id}" || logs[0]["status"] != float64(201) || logs[0]["response_bytes"] != float64(2) {
		t.Fatal("incorrect route/status/byte accounting")
	}
	for _, entry := range logs {
		if entry["event"] != "http.request_completed" || entry["outcome"] != "completed" || entry["duration_ms"].(float64) < 0 {
			t.Fatal("invalid completion")
		}
		if len(entry) != 13 {
			t.Fatalf("unexpected request log fields: %v", entry)
		}
	}
	if logs[1]["route"] != "unmatched" || logs[3]["method"] != "OTHER" {
		t.Fatal("unsafe fallback")
	}
}

func TestRequestIDs(t *testing.T) {
	router := testRouter(t, nil, io.Discard, func(r chi.Router) {
		r.Get("/id", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Captured-ID", RequestID(r.Context())) })
	})
	for _, tt := range []struct {
		name   string
		values []string
		keep   bool
	}{
		{"absent", nil, false}, {"valid", []string{"caller-id_123"}, true}, {"empty", []string{""}, false},
		{"oversized", []string{strings.Repeat("a", 65)}, false}, {"comma", []string{"first,second"}, false},
		{"duplicate", []string{"one", "two"}, false}, {"newline", []string{"bad\nvalue"}, false}, {"whitespace", []string{" value "}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v1/id", nil)
			r.Header["X-Request-Id"] = tt.values
			router.ServeHTTP(w, r)
			id := w.Header().Get("X-Request-ID")
			if !validRequestID(id) || id != w.Header().Get("Captured-ID") {
				t.Fatal("ID not propagated")
			}
			if tt.keep {
				if id != tt.values[0] {
					t.Fatal("valid caller ID replaced")
				}
			} else if len(id) != 32 {
				t.Fatal("replacement lacks random 128-bit encoding")
			}
		})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/id", nil)
	r.Header["X-Request-Id"] = []string{"one"}
	r.Header["x-request-id"] = []string{"two"}
	router.ServeHTTP(w, r)
	if len(w.Header().Get("X-Request-ID")) != 32 {
		t.Fatal("ambiguous casing accepted")
	}
	var wg sync.WaitGroup
	ids := make(chan string, 128)
	for range 128 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/id", nil))
			ids <- w.Header().Get("X-Request-ID")
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatal("duplicate generated ID")
		}
		seen[id] = true
	}
	if RequestID(context.Background()) != "" {
		t.Fatal("missing context ID not empty")
	}
}

func TestPanicsAndCapacityRelease(t *testing.T) {
	var out bytes.Buffer
	router := testRouter(t, map[string]string{"HTTP_MAX_CONCURRENT_REQUESTS": "1"}, &out, func(r chi.Router) {
		r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("PRIVATE_SENTINEL") })
		r.Get("/partial", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(202)
			_, _ = io.WriteString(w, "part")
			panic("PRIVATE_SENTINEL")
		})
		r.Get("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/panic", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"INTERNAL_ERROR"`) {
		t.Fatal("unsafe panic response")
	}
	w = httptest.NewRecorder()
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Fatal("committed panic did not abort")
			}
		}()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/partial", nil))
	}()
	if w.Code != 202 || w.Body.String() != "part" {
		t.Fatal("partial response overwritten")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ok", nil))
	if w.Code != 204 {
		t.Fatal("panic leaked admission slot")
	}
	logs := records(t, out.String())
	completions := 0
	for _, record := range logs {
		if record["request_id"] == "" {
			t.Fatal("missing correlation")
		}
		if record["event"] == "http.request_completed" {
			completions++
			if record["route"] == "/api/v1/partial" && (record["status"] != float64(202) || record["response_bytes"] != float64(4)) {
				t.Fatal("incorrect post-panic accounting")
			}
		}
	}
	if completions != 3 || len(logs) != 6 || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("incorrect panic log behavior")
	}
}

func TestBodyLimits(t *testing.T) {
	var calls atomic.Int32
	responses, err := response.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	router := testRouter(t, map[string]string{"HTTP_MAX_BODY_BYTES": "1024"}, io.Discard, func(r chi.Router) {
		r.Post("/read", func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			_, err := io.Copy(io.Discard, r.Body)
			if IsBodyTooLarge(err) {
				infrastructureError(responses, w, r, apperror.PayloadTooLarge)
				return
			}
			if err != nil {
				infrastructureError(responses, w, r, apperror.InvalidRequest)
				return
			}
			w.WriteHeader(204)
		})
	})
	for _, tt := range []struct {
		size    int
		chunked bool
		want    int
	}{{1024, false, 204}, {1025, false, 413}, {1025, true, 413}} {
		r := httptest.NewRequest("POST", "/api/v1/read", strings.NewReader(strings.Repeat("x", tt.size)))
		if tt.chunked {
			r.ContentLength = -1
			r.TransferEncoding = []string{"chunked"}
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("body result = %d", w.Code)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("known oversized body entered handler")
	}
	if IsBodyTooLarge(errors.New("other")) {
		t.Fatal("wrong error classification")
	}
}

func TestAdmissionAndDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	router := testRouter(t, map[string]string{"HTTP_MAX_CONCURRENT_REQUESTS": "1"}, io.Discard, func(r chi.Router) {
		r.Get("/block", func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) })
		r.Get("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	go func() {
		defer close(done)
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/block", nil))
	}()
	await(t, entered)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ok", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || w.Header().Get("X-Request-ID") == "" {
		t.Fatal("overload was queued or uncorrelated")
	}
	close(release)
	await(t, done)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ok", nil))
	if w.Code != 204 {
		t.Fatal("slot not released")
	}
	router = testRouter(t, map[string]string{"HTTP_REQUEST_TIMEOUT": "1ms"}, io.Discard, func(r chi.Router) {
		r.Get("/deadline", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/deadline", nil))
	if w.Code != 504 {
		t.Fatal("cooperative deadline not mapped before commit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observed := false
	router = testRouter(t, nil, io.Discard, func(r chi.Router) {
		r.Get("/cancel", func(w http.ResponseWriter, r *http.Request) {
			observed = errors.Is(r.Context().Err(), context.Canceled)
		})
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/cancel", nil).WithContext(ctx))
	if !observed {
		t.Fatal("client cancellation lost")
	}
}

func TestClientIPSpoofing(t *testing.T) {
	for _, tt := range []struct{ remote, want string }{{"127.0.0.1:1234", "127.0.0.1"}, {"[::1]:1234", "::1"}, {"[::ffff:192.0.2.1]:1234", "192.0.2.1"}, {"invalid", ""}, {"[fe80::1%en0]:1234", ""}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		r.Header.Set("X-Forwarded-For", "203.0.113.99")
		r.Header.Set("X-Real-IP", "203.0.113.99")
		r.Header.Set("Forwarded", "for=203.0.113.99")
		ip, ok := ClientIP(r)
		if tt.want == "" {
			if ok {
				t.Fatal("invalid peer accepted")
			}
		} else if !ok || ip.String() != tt.want {
			t.Fatal("forwarded header trusted or IP invalid")
		}
	}
}

type capableWriter struct {
	*httptest.ResponseRecorder
	pushed, hijacked bool
}

func (w *capableWriter) Push(string, *http.PushOptions) error { w.pushed = true; return nil }
func (w *capableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, errors.New("test")
}
func (w *capableWriter) ReadFrom(r io.Reader) (int64, error) { return io.Copy(w.ResponseRecorder, r) }

func TestWriterCapabilitiesAndInformationalHeaders(t *testing.T) {
	for _, proto := range []int{1, 2} {
		var out bytes.Buffer
		original := &capableWriter{ResponseRecorder: httptest.NewRecorder()}
		router := testRouter(t, nil, &out, func(r chi.Router) {
			r.Get("/writer", func(w http.ResponseWriter, r *http.Request) {
				if w.(interface{ Unwrap() http.ResponseWriter }).Unwrap() != original {
					t.Error("unwrap lost")
				}
				if proto == 1 {
					_, _, _ = w.(http.Hijacker).Hijack()
					_, _ = w.(io.ReaderFrom).ReadFrom(strings.NewReader("abc"))
				} else {
					_ = w.(http.Pusher).Push("/safe", nil)
				}
				w.(http.Flusher).Flush()
			})
		})
		r := httptest.NewRequest("GET", "/api/v1/writer", nil)
		r.ProtoMajor = proto
		router.ServeHTTP(original, r)
		entry := records(t, out.String())[0]
		if entry["status"] != float64(200) || (proto == 1 && (!original.hijacked || entry["response_bytes"] != float64(3))) || (proto == 2 && !original.pushed) {
			t.Fatal("writer capability/accounting lost")
		}
	}
}

func TestRequestLogFiltering(t *testing.T) {
	var out bytes.Buffer
	router := testRouter(t, map[string]string{"LOG_LEVEL": "ERROR"}, &out, nil)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if out.Len() != 0 {
		t.Fatal("completion event ignored log threshold")
	}
}

func TestEmptyRouterAndEscapedMethodPaths(t *testing.T) {
	var out bytes.Buffer
	router, err := NewRouter(testConfig(t, nil).HTTP, slog.New(slog.NewTextHandler(&out, nil)), &Lifecycle{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 404 || w.Header().Get("X-Request-ID") == "" || !strings.Contains(out.String(), "http.request_completed") {
		t.Fatal("empty router bypassed middleware")
	}
	router = testRouter(t, nil, io.Discard, func(r chi.Router) { r.Get("/items/{id}", func(http.ResponseWriter, *http.Request) {}) })
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/items/a%2Fb", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET" {
		t.Fatal("escaped path lost Allow semantics")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("UNKNOWN", "/absent", nil))
	if w.Code != 404 {
		t.Fatal("unknown route produced 405 without any allowed method")
	}
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("synchronization timed out")
	}
}

func TestCompletionOutcomesOverTCP(t *testing.T) {
	for _, tt := range []struct {
		name, outcome string
		status        int
	}{
		{"no write", "completed", 200}, {"abort before", "aborted", 0},
		{"abort after", "aborted", 202}, {"recovered panic", "completed", 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			router := testRouter(t, map[string]string{"HTTP_MAX_CONCURRENT_REQUESTS": "1"}, &out, func(api chi.Router) {
				api.Get("/test", func(w http.ResponseWriter, r *http.Request) {
					switch tt.name {
					case "abort before":
						panic(http.ErrAbortHandler)
					case "abort after":
						w.WriteHeader(http.StatusAccepted)
						if _, err := io.WriteString(w, "partial"); err != nil {
							t.Error(err)
						}
						w.(http.Flusher).Flush()
						panic(http.ErrAbortHandler)
					case "recovered panic":
						panic("PRIVATE_SENTINEL")
					}
				})
				api.Get("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
			})
			// Synchronize log reads with handler exit, including panic unwinding.
			finished := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() { finished <- struct{}{} }()
				router.ServeHTTP(w, r)
			}))
			defer server.Close()
			transport := &http.Transport{DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			resp, requestErr := client.Get(server.URL + "/api/v1/test")
			var body []byte
			if tt.name == "abort before" {
				if resp != nil || !errors.Is(requestErr, io.EOF) {
					t.Fatalf("pre-commit abort: response=%v err=%v", resp, requestErr)
				}
			} else {
				if requestErr != nil {
					t.Fatal(requestErr)
				}
				var readErr error
				body, readErr = io.ReadAll(resp.Body)
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != tt.status {
					t.Fatalf("wire status=%d; want %d", resp.StatusCode, tt.status)
				}
				switch tt.name {
				case "abort after":
					if string(body) != "partial" || !errors.Is(readErr, io.ErrUnexpectedEOF) {
						t.Fatalf("partial abort: %q, %v", body, readErr)
					}
				case "no write":
					if readErr != nil || len(body) != 0 {
						t.Fatalf("implicit 200: %q, %v", body, readErr)
					}
				case "recovered panic":
					if readErr != nil || !bytes.Contains(body, []byte(`"code":"INTERNAL_ERROR"`)) {
						t.Fatalf("recovery: %q, %v", body, readErr)
					}
				}
			}
			await(t, finished)
			logs := records(t, out.String())
			completions := 0
			for _, entry := range logs {
				if entry["event"] != "http.request_completed" {
					continue
				}
				completions++
				if entry["outcome"] != tt.outcome || entry["status"] != float64(tt.status) || entry["response_bytes"] != float64(len(body)) || entry["request_id"] == "" {
					t.Fatalf("incorrect completion: %v", entry)
				}
			}
			expectedEvents := 1
			if tt.name == "recovered panic" {
				expectedEvents = 3
			}
			if completions != 1 || len(logs) != expectedEvents || strings.Contains(out.String()+string(body), "PRIVATE_SENTINEL") {
				t.Fatal("duplicate/missing event or sensitive details")
			}

			// A second request must be admitted after either return or abort.
			resp, err := client.Get(server.URL + "/api/v1/ok")
			if err != nil {
				t.Fatal(err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			await(t, finished)
			logs = records(t, out.String())
			last := logs[len(logs)-1]
			if resp.StatusCode != 204 || len(logs) != expectedEvents+1 || last["event"] != "http.request_completed" || last["outcome"] != "completed" || last["status"] != float64(204) {
				t.Fatal("admission release or completion count")
			}
		})
	}
}
