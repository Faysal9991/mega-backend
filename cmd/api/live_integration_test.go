//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/live"
)

// This uses a closed LOCAL Redis endpoint and the exact guarded PostgreSQL
// fixture. No real Redis/LiveKit credentials or remote cache acknowledgement is
// needed, and no server shared with another application is stopped.
func TestIntegrationLiveAPIUnavailableCache(t *testing.T) {
	credentials, _, assign := step9RedisFixture(t, live.RoomModerate, live.AgencyManage, live.PolicyManage)
	values := map[string]string{"APP_ENV": "test", "LOG_FORMAT": "json", "LIVEKIT_ENABLED": "false", "REDIS_ENABLED": "true", "REDIS_STARTUP_TIMEOUT": "100ms", "REDIS_DIAL_TIMEOUT": "50ms", "HTTP_REQUEST_TIMEOUT": "30s", "LIVE_DATABASE_TIMEOUT": "30s"}
	for _, key := range []string{"HOST", "PORT", "NAME", "USER", "PASSWORD", "SSLMODE", "SSLROOTCERT", "MAX_CONNS", "CONNECT_TIMEOUT"} {
		if v, ok := os.LookupEnv("TEST_DB_" + key); ok {
			values["DB_"+key] = v
		}
	}
	unused, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	values["REDIS_URL"] = "rediss://default:synthetic-fixture-password@" + unused.Addr().String() + "/0"
	if e := unused.Close(); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	started := make(chan struct{})
	go func() {
		done <- run(ctx, func(k string) (string, bool) { v, ok := values[k]; return v, ok }, &stdout, &stderr, func(string, string) (net.Listener, error) { close(started); return listener, nil })
	}()
	defer func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 || stderr.Len() != 0 {
				t.Error("actual API startup/shutdown failed; credentials suppressed")
			}
			if !strings.Contains(stdout.String(), "cache.startup_unavailable") {
				t.Error("missing safe optional-cache diagnostic")
			}
			for _, secret := range []string{values["DB_PASSWORD"], values["REDIS_URL"]} {
				if secret != "" && strings.Contains(stdout.String()+stderr.String(), secret) {
					t.Error("secret leaked")
				}
			}
		case <-time.After(15 * time.Second):
			t.Error("actual API shutdown did not finish")
		}
	}()
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		t.Fatal("API did not bind after schema readiness")
	}
	client := &http.Client{Timeout: 35 * time.Second}
	defer client.CloseIdleConnections()
	request := func(path string, want int) {
		t.Helper()
		req, e := http.NewRequest("GET", "http://"+listener.Addr().String()+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+credentials.Access)
		req.Header.Set("X-Installation-ID", credentials.Installation)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal("actual API request failed")
		}
		defer res.Body.Close()
		var envelope map[string]any
		if e := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope); e != nil || res.StatusCode != want {
			t.Fatalf("actual API %s status=%d wanted=%d", path, res.StatusCode, want)
		}
	}
	for _, path := range []string{"/api/v1/rooms?limit=1", "/api/v1/agency-membership/history?limit=1", "/api/v1/admin/rooms?limit=1", "/api/v1/admin/agencies?limit=1", "/api/v1/admin/live-policies", "/api/v1/wallet", "/health/ready"} {
		request(path, 200)
	}
	assign(false)
	request("/api/v1/admin/rooms?limit=1", 403)
	request("/api/v1/admin/agencies?limit=1", 403)
	request("/api/v1/rooms?limit=1", 200)
	assign(true)
	request("/api/v1/admin/rooms?limit=1", 200)
}
