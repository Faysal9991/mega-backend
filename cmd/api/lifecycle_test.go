package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready := &readyListener{Listener: listener, ready: make(chan struct{})}
	result := make(chan int, 1)
	go func() {
		result <- run(ctx, func(string) (string, bool) { return "", false }, &stdout, &stderr, func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != "127.0.0.1:8080" {
				t.Error("bad listen arguments")
			}
			return ready, nil
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

	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatalf("lifecycle exit = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stalled")
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
	code := run(context.Background(), func(string) (string, bool) { return "", false }, &stdout, &stderr, func(string, string) (net.Listener, error) { return nil, errors.New("PRIVATE_SENTINEL") })
	if code != 1 || !strings.Contains(stdout.String(), "http.listener_failed") || !strings.Contains(stderr.String(), "listener binding failed") || strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe or missing bind failure")
	}
}

type failedAcceptListener struct{ net.Listener }

func (f failedAcceptListener) Accept() (net.Conn, error) { return nil, errors.New("PRIVATE_SENTINEL") }

func TestRunUnexpectedServerFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	code := run(context.Background(), func(string) (string, bool) { return "", false }, &stdout, &stderr, func(string, string) (net.Listener, error) { return failedAcceptListener{listener}, nil })
	if code != 1 || !strings.Contains(stdout.String(), "http.server_failed") || strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unexpected server error was not safely propagated")
	}
}
