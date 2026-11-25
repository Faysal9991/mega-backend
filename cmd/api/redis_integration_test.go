//go:build redis_integration

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

// Uses the actual composition root, PostgreSQL startup gate, Redis adapter and
// TCP HTTP server. Failures are local; the shared Redis server is never stopped.
func TestRedisIntegrationAPILifecycle(t *testing.T) {
	if os.Getenv("BIHO_REDIS_INTEGRATION_ACK") != "169.58.127.58:7200/0" {
		t.Fatal("authorized endpoint acknowledgment required")
	}
	cfg, err := config.LoadRedis(os.LookupEnv, config.Test)
	if err != nil || !cfg.Enabled {
		t.Fatal("enabled integration configuration required; credentials suppressed")
	}
	endpoint, err := config.RedisEndpoint(cfg.URL.Value())
	if err != nil || endpoint.Hostname() != "169.58.127.58" || endpoint.Port() != "7200" || endpoint.User.Username() != "default" {
		t.Fatal("unauthorized integration target")
	}
	for _, mode := range []string{"enabled", "disabled", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			values := map[string]string{"APP_ENV": "test", "LOG_FORMAT": "json"}
			for _, key := range []string{"HOST", "PORT", "NAME", "USER", "PASSWORD", "SSLMODE", "SSLROOTCERT", "MAX_CONNS", "CONNECT_TIMEOUT"} {
				if v, ok := os.LookupEnv("TEST_DB_" + key); ok {
					values["DB_"+key] = v
				}
			}
			for _, key := range []string{"REDIS_ENABLED", "REDIS_URL", "REDIS_TLS_INSECURE_SKIP_VERIFY"} {
				values[key] = os.Getenv(key)
			}
			if mode == "disabled" {
				values["REDIS_ENABLED"] = "false"
				values["REDIS_URL"] = "invalid-unread-credential"
			}
			if mode == "unavailable" {
				unused, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				values["REDIS_URL"] = "rediss://default:synthetic-password@" + unused.Addr().String() + "/0"
				if err := unused.Close(); err != nil {
					t.Fatal(err)
				}
				values["REDIS_STARTUP_TIMEOUT"] = "100ms"
				values["REDIS_DIAL_TIMEOUT"] = "50ms"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var stdout, stderr bytes.Buffer
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ready := &readyListener{Listener: listener, ready: make(chan struct{})}
			done := make(chan int, 1)
			go func() {
				done <- run(ctx, func(k string) (string, bool) { v, ok := values[k]; return v, ok }, &stdout, &stderr, func(string, string) (net.Listener, error) { return ready, nil })
			}()
			// Always join before inspecting non-thread-safe log buffers, even on failure.
			defer func() {
				cancel()
				select {
				case code := <-done:
					if code != 0 || stderr.Len() != 0 {
						t.Error("API lifecycle failed; diagnostics suppressed")
					}
					wantWarning := mode == "unavailable"
					if strings.Contains(stdout.String(), "cache.startup_unavailable") != wantWarning {
						t.Error("unexpected optional cache startup diagnostic")
					}
					for _, secret := range []string{cfg.URL.Value(), values["DB_PASSWORD"]} {
						if secret != "" && strings.Contains(stdout.String()+stderr.String(), secret) {
							t.Error("secret leaked in runtime diagnostics")
						}
					}
				case <-time.After(10 * time.Second):
					t.Error("API graceful shutdown timed out")
				}
			}()
			select {
			case <-ready.ready:
			case <-time.After(15 * time.Second):
				t.Fatal("API did not reach HTTP serving")
			}
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			for _, probe := range []struct {
				path   string
				status int
			}{{"/health/live", http.StatusOK}, {"/health/ready", http.StatusOK}, {"/api/v1/identity/me", http.StatusUnauthorized}} {
				response, err := client.Get("http://" + listener.Addr().String() + probe.path)
				if err != nil {
					t.Fatal("health request failed")
				}
				_, readErr := io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil || response.StatusCode != probe.status {
					t.Error("health/readiness failed")
				}
			}
		})
	}
}
