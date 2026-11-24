package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/go-chi/chi/v5"
)

func healthRouter(t *testing.T, state *Lifecycle, out io.Writer, register func(chi.Router)) http.Handler {
	t.Helper()
	cfg := testConfig(t, map[string]string{"LOG_FORMAT": "json", "HTTP_MAX_CONCURRENT_REQUESTS": "1"})
	logger, err := logging.New(cfg, out)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(cfg.HTTP, logger, state, register)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func TestHealthContracts(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/fixtures/health.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		State, Path string
		Status      int
		Body        map[string]any
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.State+f.Path, func(t *testing.T) {
			state := &Lifecycle{}
			switch f.State {
			case "ready":
				state.markReady()
			case "draining":
				state.markReady()
				state.markUnready()
			}
			router := healthRouter(t, state, io.Discard, nil)
			var getLength string
			for _, method := range []string{"GET", "HEAD"} {
				rec := httptest.NewRecorder()
				r := httptest.NewRequest(method, f.Path, nil)
				r.Header.Set("X-Request-ID", "health-test")
				router.ServeHTTP(rec, r)
				if rec.Code != f.Status || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Request-ID") != "health-test" {
					t.Fatalf("probe status/headers: %d, %v", rec.Code, rec.Header())
				}
				if f.Status == 503 && rec.Header().Get("Retry-After") != "1" {
					t.Fatal("missing retry hint")
				}
				if method == "HEAD" {
					if rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != getLength {
						t.Fatal("HEAD mismatch")
					}
					continue
				}
				getLength = rec.Header().Get("Content-Length")
				if getLength != strconv.Itoa(rec.Body.Len()) {
					t.Fatal("content length")
				}
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(body, f.Body) {
					t.Fatalf("contract fixture drift: %s", rec.Body)
				}
			}
		})
	}
	state := &Lifecycle{}
	router := healthRouter(t, state, io.Discard, nil)
	for _, path := range []string{"/health/live", "/health/ready"} {
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS", "PRIVATE_SENTINEL"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			checkErrorContract(t, rec, "METHOD_NOT_ALLOWED")
			if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("probe method contract")
			}
		}
	}
	// Probes retain body limits and exact routing; they do not create business APIs.
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health/live", nil)
	r.ContentLength = 2 << 20
	router.ServeHTTP(rec, r)
	if rec.Code != 413 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("probe bypassed body limit")
	}
	for _, path := range []string{"/api/v1/health/live", "/health/live/", "/health/%6cive"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 404 {
			t.Fatal("unexpected probe alias", path)
		}
	}
}

func TestHealthIndependentOfAdmission(t *testing.T) {
	state := &Lifecycle{}
	state.markReady()
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := healthRouter(t, state, io.Discard, func(api chi.Router) {
		api.Get("/hold", func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(204) })
	})
	go func() {
		defer close(finished)
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/hold", nil))
	}()
	await(t, entered)
	defer func() { close(release); await(t, finished) }()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/hold", nil))
	if rec.Code != 503 {
		t.Fatal("admission not saturated")
	}
	for _, draining := range []bool{false, true} {
		if draining {
			state.markUnready()
		}
		for _, path := range []string{"/health/live", "/health/ready"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
				want := 200
				if path == "/health/ready" && draining {
					want = 503
				}
				if method == "POST" {
					want = 405
				}
				if rec.Code != want {
					t.Fatalf("saturated %s %s: got %d, want %d", method, path, rec.Code, want)
				}
			}
		}
	}
}

func TestLifecycleConcurrentAndTerminal(t *testing.T) {
	state := &Lifecycle{}
	if state.Ready() {
		t.Fatal("initially ready")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 100 {
				state.markReady()
				_ = state.Ready()
				state.markUnready()
			}
		})
	}
	wg.Wait()
	if state.Ready() {
		t.Fatal("ready after stop")
	}
	for range 32 {
		wg.Go(func() {
			state.markReady()
			if state.Ready() {
				t.Error("terminal state resurrected")
			}
		})
	}
	wg.Wait()
}

type healthClosingListener struct {
	net.Listener
	state   *Lifecycle
	closing chan bool
	once    sync.Once
}

func (l *healthClosingListener) Close() error {
	l.once.Do(func() { l.closing <- l.state.Ready() })
	return l.Listener.Close()
}

func TestHealthServeAndDrain(t *testing.T) {
	state := &Lifecycle{}
	cfg := testConfig(t, map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "5s"})
	logger, err := logging.New(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(cfg.HTTP, logger, state)
	if err != nil {
		t.Fatal(err)
	}
	// Hold already-accepted probe requests before routing, then release after the
	// listener closes. New connections after shutdown are not promised a 503.
	var hold atomic.Bool
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hold.Load() {
			entered <- struct{}{}
			<-release
		}
		router.ServeHTTP(w, r)
	})
	server, err := New(cfg.HTTP, logger, state, handler)
	if err != nil {
		t.Fatal(err)
	}
	if state.Ready() {
		t.Fatal("construction marked ready")
	}
	listener := &healthClosingListener{Listener: loopback(t), state: state, closing: make(chan bool, 1)}
	if state.Ready() {
		t.Fatal("binding alone marked ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- server.Run(ctx, listener) }()
	defer func() { releaseOnce.Do(func() { close(release) }); _ = server.Close() }()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + listener.Addr().String() + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !state.Ready() {
		t.Fatal("Serve not ready")
	}
	hold.Store(true)
	type result struct {
		path   string
		status int
		err    error
	}
	results := make(chan result, 2)
	for _, path := range []string{"/health/live", "/health/ready"} {
		go func() {
			resp, err := client.Get("http://" + listener.Addr().String() + path)
			status := 0
			if err == nil {
				status = resp.StatusCode
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			results <- result{path, status, err}
		}()
	}
	await(t, entered)
	await(t, entered)
	cancel()
	select {
	case readyAtClose := <-listener.closing:
		if readyAtClose {
			t.Fatal("ready when listener close began")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stalled")
	}
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		select {
		case got := <-results:
			want := 200
			if got.path == "/health/ready" {
				want = 503
			}
			if got.err != nil || got.status != want {
				t.Fatalf("draining probe: %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("probe stalled")
		}
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drain stalled")
	}
	if state.Ready() {
		t.Fatal("ready after shutdown")
	}
}

func TestHealthStateOnLifecycleFailures(t *testing.T) {
	cfg := testConfig(t, nil)
	logger, err := logging.New(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRouter(cfg.HTTP, logger, nil); err == nil {
		t.Fatal("nil lifecycle router")
	}
	if _, err := New(cfg.HTTP, logger, nil, http.NotFoundHandler()); err == nil {
		t.Fatal("nil lifecycle server")
	}
	for _, mode := range []string{"nil listener", "accept failure", "canceled startup", "close before serve", "shutdown before serve"} {
		t.Run(mode, func(t *testing.T) {
			state := &Lifecycle{}
			server, err := New(cfg.HTTP, logger, state, http.NotFoundHandler())
			if err != nil {
				t.Fatal(err)
			}
			listener := loopback(t)
			switch mode {
			case "nil listener":
				if server.Serve(nil) == nil {
					t.Fatal("nil listener accepted")
				}
			case "accept failure":
				if server.Serve(failingListener{listener, io.ErrUnexpectedEOF}) == nil {
					t.Fatal("accept failure lost")
				}
			case "canceled startup":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := server.Run(ctx, listener); err != nil {
					t.Fatal(err)
				}
			case "close before serve":
				if err := server.Close(); err != nil {
					t.Fatal(err)
				}
				if err := server.Serve(listener); err != nil {
					t.Fatal(err)
				}
			case "shutdown before serve":
				if err := server.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := server.Serve(listener); err != nil {
					t.Fatal(err)
				}
			}
			if state.Ready() {
				t.Fatal("failed or stopped server remains ready")
			}
		})
	}
}

func TestHealthCompletionLogging(t *testing.T) {
	var out bytes.Buffer
	router := healthRouter(t, &Lifecycle{}, &out, nil)
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/health/live", 200}, {"HEAD", "/health/ready", 503}, {"POST", "/health/ready", 405},
	} {
		r := httptest.NewRequest(tt.method, tt.path+"?secret=PRIVATE_SENTINEL", nil)
		r.Header.Set("Authorization", "PRIVATE_SENTINEL")
		r.Header.Set("X-Request-ID", "probe-id")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != tt.status {
			t.Fatal("probe status")
		}
	}
	logs := records(t, out.String())
	if len(logs) != 3 || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("probe completion/privacy")
	}
	for _, entry := range logs {
		if entry["event"] != "http.request_completed" || entry["outcome"] != "completed" || entry["request_id"] != "probe-id" {
			t.Fatal("probe bypassed shared logging")
		}
	}
	if logs[0]["route"] != "/health/live" || logs[1]["route"] != "/health/ready" || logs[1]["response_bytes"] != float64(0) {
		t.Fatal("probe route/HEAD accounting")
	}
}

func TestDependencyReadinessLossRecoveryAndShutdown(t *testing.T) {
	var healthy atomic.Bool
	var checks atomic.Int64
	healthy.Store(true)
	state := NewLifecycle(func(context.Context) bool { checks.Add(1); return healthy.Load() })
	router := healthRouter(t, state, io.Discard, nil)
	probe := func(method, path string, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s = %d", method, path, rec.Code)
		}
		if want == 503 && method != "HEAD" {
			checkErrorContract(t, rec, "SERVICE_UNAVAILABLE")
		}
		if method == "HEAD" && rec.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	probe("GET", "/health/ready", 503)
	if checks.Load() != 0 {
		t.Fatal("dependency checked before running")
	}
	state.markReady()
	probe("GET", "/health/ready", 200)
	healthy.Store(false)
	probe("HEAD", "/health/ready", 503)
	probe("GET", "/health/live", 200)
	if !state.Ready() {
		t.Fatal("outage made lifecycle terminal")
	}
	healthy.Store(true)
	probe("GET", "/health/ready", 200)
	// Use the real shutdown entry point while a successful check is in flight.
	entered, release := make(chan struct{}), make(chan struct{})
	blocked := NewLifecycle(func(context.Context) bool { close(entered); <-release; return true })
	blocked.markReady()
	blockedRouter := healthRouter(t, blocked, io.Discard, nil)
	cfg := testConfig(t, nil)
	logger, err := logging.New(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg.HTTP, logger, blocked, blockedRouter)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		blockedRouter.ServeHTTP(rec, httptest.NewRequest("GET", "/health/ready", nil))
		result <- rec
	}()
	await(t, entered)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	rec := <-result
	if rec.Code != 503 || blocked.Ready() {
		t.Fatal("in-flight recovery restored readiness after shutdown")
	}
	for _, path := range []string{"/health/live", "/health/ready"} {
		rec := httptest.NewRecorder()
		blockedRouter.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		want := 200
		if path == "/health/ready" {
			want = 503
		}
		if rec.Code != want {
			t.Fatal("post-shutdown handler contract")
		}
	}
}
