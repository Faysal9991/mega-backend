package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type readyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}

func TestRunSuccessfulLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	var databaseHealthy, databaseClosed atomic.Bool
	databaseHealthy.Store(true)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready := &readyListener{Listener: listener, ready: make(chan struct{})}
	result := make(chan int, 1)
	go func() {
		result <- runWithDatabase(ctx, databaseTestLookup, &stdout, &stderr, func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != "127.0.0.1:8080" {
				t.Error("bad listen arguments")
			}
			return ready, nil
		}, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
			return databaseRuntime{close: func() { databaseClosed.Store(true) }, available: func(context.Context) bool { return databaseHealthy.Load() }}, nil
		})
	}()
	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("startup stalled")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/not-a-business-route")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 404 || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("startup router unavailable")
	}
	for _, path := range []string{"/health/live", "/health/ready"} {
		for _, method := range []string{"GET", "HEAD"} {
			request, err := http.NewRequest(method, "http://"+listener.Addr().String()+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			probe, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(probe.Body)
			_ = probe.Body.Close()
			if err != nil || probe.StatusCode != 200 || probe.Header.Get("Cache-Control") != "no-store" || probe.Header.Get("X-Request-ID") == "" {
				t.Fatal("startup health probe failed")
			}
			if method == "HEAD" && len(body) != 0 {
				t.Fatal("HEAD probe returned a body")
			}
		}
	}

	for _, tt := range []struct {
		healthy bool
		path    string
		want    int
	}{
		{false, "/health/ready", 503}, {false, "/health/live", 200}, {true, "/health/ready", 200},
	} {
		databaseHealthy.Store(tt.healthy)
		response, err := client.Get("http://" + listener.Addr().String() + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != tt.want {
			t.Fatal("runtime database availability was not wired into probes")
		}
	}
	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("lifecycle exit = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stalled")
	}
	if !databaseClosed.Load() {
		t.Fatal("database dependency not closed after HTTP shutdown")
	}
	if stderr.Len() != 0 {
		t.Fatal("unexpected safe startup error")
	}
	for _, event := range []string{"startup.configuration_validated", "http.listener_bound", "http.request_completed", "http.shutdown_completed"} {
		if !strings.Contains(stdout.String(), event) {
			t.Fatalf("missing event %s", event)
		}
	}
	if strings.Contains(stdout.String(), "http_server_implemented=false") {
		t.Fatal("outdated startup state")
	}
}

func TestRunListenerFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDatabase(context.Background(), databaseTestLookup, &stdout, &stderr, func(string, string) (net.Listener, error) { return nil, errors.New("PRIVATE_SENTINEL") }, fakeDatabaseStart)
	if code != 1 || !strings.Contains(stdout.String(), "http.listener_failed") || !strings.Contains(stderr.String(), "listener binding failed") || strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe or missing bind failure")
	}
}

type failedAcceptListener struct{ net.Listener }

func (f failedAcceptListener) Accept() (net.Conn, error) {
	return nil, errors.New("PRIVATE_SENTINEL")
}

func TestRunUnexpectedServerFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	code := runWithDatabase(context.Background(), databaseTestLookup, &stdout, &stderr, func(string, string) (net.Listener, error) { return failedAcceptListener{listener}, nil }, fakeDatabaseStart)
	if code != 1 || !strings.Contains(stdout.String(), "http.server_failed") || strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unexpected server error was not safely propagated")
	}
}

func databaseTestLookup(key string) (string, bool) {
	values := map[string]string{"DB_HOST": "127.0.0.1", "DB_NAME": "biho_live_dev", "DB_USER": "biho_live_dev_app", "DB_PASSWORD": "synthetic-test-only", "DB_SSLMODE": "require"}
	v, ok := values[key]
	return v, ok
}
func fakeDatabaseStart(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
	return databaseRuntime{close: func() {}, available: func(context.Context) bool { return true }}, nil
}

func TestDatabaseStartupBoundary(t *testing.T) {
	var out, stderr bytes.Buffer
	bound, closed := false, false
	code := runWithDatabase(context.Background(), databaseTestLookup, &out, &stderr, func(string, string) (net.Listener, error) { bound = true; return nil, errors.New("bind failed") }, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
		return databaseRuntime{}, errors.New("PRIVATE_SENTINEL")
	})
	if code != 1 || bound || strings.Contains(out.String()+stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("database startup failure bound listener or leaked cause")
	}
	out.Reset()
	stderr.Reset()
	code = runWithDatabase(context.Background(), databaseTestLookup, &out, &stderr, func(string, string) (net.Listener, error) { bound = true; return nil, errors.New("bind failed") }, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
		return databaseRuntime{close: func() { closed = true }, available: func(context.Context) bool { return true }}, nil
	})
	if code != 1 || !bound || !closed {
		t.Fatal("database pool not closed on listener failure")
	}
	out.Reset()
	stderr.Reset()
	bound = false
	code = runWithDatabase(context.Background(), func(key string) (string, bool) {
		if key == "DB_SSLMODE" {
			return "disable", true
		}
		return databaseTestLookup(key)
	}, &out, &stderr, func(string, string) (net.Listener, error) { bound = true; return nil, nil }, fakeDatabaseStart)
	if code != 1 || bound {
		t.Fatal("invalid database TLS accepted at startup")
	}
}

func TestDatabaseInitializationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, stderr bytes.Buffer
	code := runWithDatabase(ctx, databaseTestLookup, &out, &stderr, func(string, string) (net.Listener, error) {
		t.Fatal("canceled initialization bound a listener")
		return nil, nil
	}, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
		cancel()
		return databaseRuntime{}, errors.New("PRIVATE_SENTINEL")
	})
	if code != 0 || stderr.Len() != 0 || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("canceled database startup was not graceful")
	}
}
