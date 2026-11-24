package httpserver

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/go-chi/chi/v5"
)

func testServer(t *testing.T, values map[string]string, handler http.Handler) *Server {
	t.Helper()
	cfg := testConfig(t, values)
	logger, err := logging.New(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg.HTTP, logger, handler)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func loopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestServerConstruction(t *testing.T) {
	cfg := testConfig(t, nil)
	logger, _ := logging.New(cfg, io.Discard)
	for _, tt := range []struct{ host, want string }{{"127.0.0.1", "127.0.0.1:8080"}, {"::1", "[::1]:8080"}} {
		cfg.HTTP.Host = tt.host
		server, err := New(cfg.HTTP, logger, http.NotFoundHandler())
		if err != nil {
			t.Fatal(err)
		}
		if server.Address() != tt.want || server.http.ReadHeaderTimeout != cfg.HTTP.ReadHeaderTimeout || server.http.ReadTimeout != cfg.HTTP.ReadTimeout || server.http.WriteTimeout != cfg.HTTP.WriteTimeout || server.http.IdleTimeout != cfg.HTTP.IdleTimeout || server.http.MaxHeaderBytes != cfg.HTTP.MaxHeaderBytes || server.shutdownTimeout != cfg.HTTP.ShutdownTimeout {
			t.Fatal("transport settings not applied")
		}
	}
	if _, err := New(cfg.HTTP, nil, http.NotFoundHandler()); err == nil {
		t.Fatal("nil logger accepted")
	}
	if _, err := New(cfg.HTTP, logger, nil); err == nil {
		t.Fatal("nil router accepted")
	}
	if _, err := NewRouter(cfg.HTTP, nil); err == nil {
		t.Fatal("nil router logger accepted")
	}
	cfg.HTTP.MaxConcurrentRequests = 0
	if _, err := NewRouter(cfg.HTTP, logger); err == nil {
		t.Fatal("unbounded admission accepted")
	}
}

func TestOversizedTransportHeaders(t *testing.T) {
	server := testServer(t, map[string]string{"HTTP_MAX_HEADER_BYTES": "1024"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("oversized header reached handler") }))
	listener := loopback(t)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nX-Large: "+strings.Repeat("a", 16384)+"\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("header response = %d", response.StatusCode)
	}
}

type closingListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *closingListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

func TestGracefulDrain(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	requestContext := make(chan context.Context, 1)
	server := testServer(t, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestContext <- r.Context()
		close(entered)
		<-release
		_, _ = io.WriteString(w, "finished")
	}))
	listener := &closingListener{Listener: loopback(t), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runResult := make(chan error, 1)
	go func() { runResult <- server.Run(ctx, listener) }()
	result := make(chan string, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			result <- "failed"
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			result <- "failed"
			return
		}
		result <- string(body)
	}()
	await(t, entered)
	reqCtx := <-requestContext
	cancel()
	await(t, listener.closed)
	if reqCtx.Err() != nil {
		t.Error("shutdown canceled in-flight context before drain")
	}
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Error("shutdown accepted new connection")
	}
	close(release)
	select {
	case body := <-result:
		if body != "finished" {
			t.Fatal("drain truncated response")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request failed to drain")
	}
	select {
	case err := <-runResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run failed to finish")
	}
}

func TestForcedShutdownCancelsConnection(t *testing.T) {
	entered := make(chan struct{})
	handlerDone := make(chan struct{})
	server := testServer(t, map[string]string{"HTTP_SHUTDOWN_TIMEOUT": "1ms"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(handlerDone) }))
	listener := loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runResult := make(chan error, 1)
	go func() { runResult <- server.Run(ctx, listener) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	await(t, entered)
	cancel()
	select {
	case err := <-runResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("forced drain result = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("forced close stalled")
	}
	await(t, handlerDone)
	await(t, clientDone)
}

type failingListener struct {
	net.Listener
	failure error
}

func (l failingListener) Accept() (net.Conn, error) { return nil, l.failure }

func TestUnexpectedServeErrorAndCanceledStartup(t *testing.T) {
	failure := errors.New("synthetic accept failure")
	server := testServer(t, nil, http.NotFoundHandler())
	if err := server.Run(context.Background(), failingListener{loopback(t), failure}); !errors.Is(err, failure) {
		t.Fatalf("serve error not propagated: %v", err)
	}
	server = testServer(t, nil, http.NotFoundHandler())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Run(ctx, loopback(t)); err != nil {
		t.Fatal(err)
	}
}

func TestResponseControllerAndPartialPanicOverTCP(t *testing.T) {
	cfg := testConfig(t, nil)
	logger, _ := logging.New(cfg, io.Discard)
	router, err := NewRouter(cfg.HTTP, logger, func(r chi.Router) {
		r.Get("/flush", func(w http.ResponseWriter, r *http.Request) {
			controller := http.NewResponseController(w)
			if err := controller.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
				t.Error(err)
			}
			_, _ = io.WriteString(w, "partial")
			if err := controller.Flush(); err != nil {
				t.Error(err)
			}
			panic("PRIVATE_SENTINEL")
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, nil, router)
	listener := loopback(t)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/api/v1/flush")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != "partial" || err == nil || response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("post-commit panic was not an aborted partial response: %d %q %v", response.StatusCode, body, err)
	}
}
