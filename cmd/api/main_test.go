package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"io"
	"net"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	for _, tt := range []struct {
		name, port string
		want       int
	}{
		{"valid defaults", "", 0},
		{"invalid config", "PRIVATE_SENTINEL", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			calls := map[string]int{}
			code := runCancelled(func(key string) (string, bool) {
				calls[key]++
				return tt.port, key == "HTTP_PORT" && tt.port != ""
			}, &stdout, &stderr)
			if code != tt.want {
				t.Fatalf("exit code %d, want %d", code, tt.want)
			}
			for key, count := range calls {
				if count != 1 {
					t.Fatalf("setting %s loaded %d times", key, count)
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
				t.Fatal("startup leaked invalid setting value")
			}
			if tt.want == 1 && (stdout.Len() != 0 || !strings.Contains(stderr.String(), "HTTP_PORT")) {
				t.Fatal("invalid configuration must fail before successful startup output")
			}
			if tt.want == 0 && (stderr.Len() != 0 || !strings.Contains(stdout.String(), "event=startup.configuration_validated") || !strings.Contains(stdout.String(), "http_server_implemented=true")) {
				t.Fatal("unexpected startup output")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestRunOutputFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := runCancelled(func(string) (string, bool) { return "", false }, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("output failure exit code = %d, want 1", code)
	}
}

func TestRunLoggingModes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values map[string]string
		code   int
		silent bool
	}{
		{"production JSON", map[string]string{"APP_ENV": "production", "APP_NAME": "api", "HTTP_HOST": "127.0.0.1", "INSTANCE_ID": "replica-1"}, 0, false},
		{"explicit JSON", map[string]string{"LOG_FORMAT": "json"}, 0, false},
		{"warn filters startup", map[string]string{"LOG_LEVEL": "WARN"}, 0, true},
		{"error filters startup", map[string]string{"LOG_LEVEL": "ERROR"}, 0, true},
		{"debug emits INFO startup", map[string]string{"LOG_LEVEL": "DEBUG", "LOG_FORMAT": "json"}, 0, false},
		{"info emits startup", map[string]string{"LOG_LEVEL": "INFO", "LOG_FORMAT": "json"}, 0, false},
		{"invalid level", map[string]string{"LOG_LEVEL": "PRIVATE_SENTINEL"}, 1, true},
		{"empty instance", map[string]string{"INSTANCE_ID": ""}, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			calls := map[string]int{}
			code := runCancelled(func(key string) (string, bool) { calls[key]++; v, ok := tt.values[key]; return v, ok }, &stdout, &stderr)
			if code != tt.code {
				t.Fatalf("exit %d, want %d", code, tt.code)
			}
			for key, count := range calls {
				if count != 1 {
					t.Fatalf("%s read more than once", key)
				}
			}
			if code == 0 && len(calls) != 16 {
				t.Fatal("startup did not load complete configuration once")
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
				t.Fatal("unsafe error")
			}
			if tt.silent {
				if stdout.Len() != 0 {
					t.Fatal("unexpected output")
				}
				return
			}
			var event map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if stderr.Len() != 0 || event["event"] != "startup.configuration_validated" || event["http_server_implemented"] != true {
				t.Fatal("invalid startup event")
			}
		})
	}
}

func TestRunLoggerConstructionFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := runCancelled(func(string) (string, bool) { return "", false }, nil, &stderr); code != 1 || !strings.Contains(stderr.String(), "writer is required") {
		t.Fatal("logger construction failure not reported")
	}
}

// Existing config/logging regression fixtures stop after startup validation;
// lifecycle tests below exercise actual listener ownership and shutdown.
func runCancelled(lookup config.LookupEnv, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return run(ctx, lookup, stdout, stderr, func(string, string) (net.Listener, error) { panic("canceled startup must not bind") })
}
