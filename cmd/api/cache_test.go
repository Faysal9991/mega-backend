package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/rediscache"
)

type cacheCloseFunc func() error

func (f cacheCloseFunc) Close() error { return f() }

type cacheListener struct {
	*readyListener
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *cacheListener) Close() error {
	err := l.Listener.Close()
	l.closeOnce.Do(func() { close(l.closed) })
	return err
}
func TestCacheFailureOptionalAndLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	var closed atomic.Int64
	var dbClosed atomic.Bool
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := &cacheListener{readyListener: &readyListener{Listener: listener, ready: make(chan struct{})}, closed: make(chan struct{})}
	unused, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := unused.Addr().String()
	_ = unused.Close()
	lookup := func(key string) (string, bool) {
		values := map[string]string{"REDIS_ENABLED": "true", "REDIS_URL": "rediss://default:PRIVATE_SENTINEL@" + address + "/0", "REDIS_DIAL_TIMEOUT": "100ms", "REDIS_STARTUP_TIMEOUT": "200ms"}
		if v, ok := values[key]; ok {
			return v, true
		}
		return databaseTestLookup(key)
	}
	result := make(chan int, 1)
	go func() {
		result <- runWithServices(ctx, lookup, &stdout, &stderr, func(string, string) (net.Listener, error) { return ready, nil }, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
			return databaseRuntime{close: func() { dbClosed.Store(true) }, available: func(context.Context) bool { return true }}, nil
		}, func(ctx context.Context, cfg config.RedisConfig, logger *slog.Logger) (io.Closer, error) {
			cache, err := rediscache.Open(ctx, cfg, logger)
			if err != nil {
				return nil, err
			}
			return cacheCloseFunc(func() error {
				closed.Add(1)
				select {
				case <-ready.closed:
				default:
					t.Error("cache closed before HTTP stopped")
				}
				if dbClosed.Load() {
					t.Error("database closed before optional cache")
				}
				return cache.Close()
			}), nil
		})
	}()
	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("cache outage prevented listener startup")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/health/live", "/health/ready"} {
		response, err := client.Get("http://" + listener.Addr().String() + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("optional cache affected liveness/PostgreSQL readiness")
		}
	}
	cancel()
	select {
	case code := <-result:
		if code != 0 {
			t.Fatal("optional cache changed exit status")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cache shutdown stalled")
	}
	if closed.Load() != 1 || !dbClosed.Load() || stderr.Len() != 0 || !strings.Contains(stdout.String(), "cache.startup_unavailable") || strings.Contains(stdout.String(), "PRIVATE_SENTINEL") {
		t.Fatal("cache lifecycle/diagnostic failure")
	}
}
func TestInvalidRedisFailsBeforeNetworkStartup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	lookup := func(key string) (string, bool) {
		if key == "REDIS_ENABLED" {
			return "true", true
		}
		if key == "REDIS_URL" {
			return "redis://PRIVATE_SENTINEL", true
		}
		return databaseTestLookup(key)
	}
	code := runWithServices(context.Background(), lookup, &stdout, &stderr, func(string, string) (net.Listener, error) {
		t.Fatal("invalid Redis configuration bound listener")
		return nil, nil
	}, func(context.Context, config.DatabaseConfig) (databaseRuntime, error) {
		t.Fatal("invalid Redis configuration initialized database")
		return databaseRuntime{}, nil
	}, func(context.Context, config.RedisConfig, *slog.Logger) (io.Closer, error) {
		t.Fatal("invalid Redis configuration constructed cache")
		return nil, nil
	})
	if code != 1 || strings.Contains(stderr.String(), "PRIVATE_SENTINEL") {
		t.Fatal("invalid Redis configuration accepted or leaked")
	}
}

func TestOptionalCacheStartupCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	closes := 0
	code := runWithServices(ctx, databaseTestLookup, &stdout, &stderr, func(string, string) (net.Listener, error) {
		t.Fatal("canceled cache initialization bound a listener")
		return nil, nil
	}, fakeDatabaseStart, func(context.Context, config.RedisConfig, *slog.Logger) (io.Closer, error) {
		cancel()
		return cacheCloseFunc(func() error { closes++; return nil }), nil
	})
	if code != 0 || closes != 1 || stderr.Len() != 0 {
		t.Fatal("canceled cache startup cleanup/exit")
	}
}
