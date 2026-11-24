package config_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func lookup(values map[string]string) config.LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestLoadValid(t *testing.T) {
	defaults := config.Config{
		Environment: config.Development, AppName: "biho-live-api",
		Logging: config.LoggingConfig{Level: slog.LevelInfo, Format: config.LogText, InstanceID: "local"},
		HTTP: config.HTTPConfig{
			Host: "127.0.0.1", Port: 8080, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
			IdleTimeout: time.Minute, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1048576,
		},
	}
	overrides := config.Config{
		Environment: config.Production, AppName: "mega-api_2.0",
		Logging: config.LoggingConfig{Level: slog.LevelInfo, Format: config.LogJSON, InstanceID: "api-1"},
		HTTP: config.HTTPConfig{
			Host: "::1", Port: 9090, ReadHeaderTimeout: time.Second,
			ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second,
			IdleTimeout: 4 * time.Minute, ShutdownTimeout: 1500 * time.Millisecond, MaxBodyBytes: 2048,
		},
	}
	tests := []struct {
		name string
		env  map[string]string
		want config.Config
	}{
		{"defaults", nil, defaults},
		{"all overrides", map[string]string{
			"APP_ENV": "production", "APP_NAME": "mega-api_2.0", "HTTP_HOST": "::1", "HTTP_PORT": "9090", "INSTANCE_ID": "api-1",
			"HTTP_READ_HEADER_TIMEOUT": "1s", "HTTP_READ_TIMEOUT": "2s", "HTTP_WRITE_TIMEOUT": "3s",
			"HTTP_IDLE_TIMEOUT": "4m", "HTTP_SHUTDOWN_TIMEOUT": "1.5s", "HTTP_MAX_BODY_BYTES": "2048",
		}, overrides},
	}
	for _, env := range []config.Environment{config.Test, config.Staging} {
		want := defaults
		want.Environment = env
		if env == config.Staging {
			want.Logging.Format = config.LogJSON
		}
		tests = append(tests, struct {
			name string
			env  map[string]string
			want config.Config
		}{string(env), map[string]string{"APP_ENV": string(env), "APP_NAME": defaults.AppName, "HTTP_HOST": defaults.HTTP.Host, "INSTANCE_ID": "local"}, want})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(lookup(tt.env))
			if err != nil || got != tt.want {
				t.Fatalf("Load() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name, key, value, reason string
	}{
		{"unsupported environment", "APP_ENV", "invalid", "must be development"},
		{"environment whitespace", "APP_ENV", " production ", "must be development"},
		{"invalid name", "APP_NAME", "name\nforged log", "ASCII"},
		{"long name", "APP_NAME", strings.Repeat("a", 65), "1 to 64"},
		{"invalid host", "HTTP_HOST", "https://private.example:80", "IPv4 or IPv6"},
		{"host with port", "HTTP_HOST", "127.0.0.1:8080", "IPv4 or IPv6"},
		{"host with zone", "HTTP_HOST", "fe80::1%en0", "IPv4 or IPv6"},
		{"port text", "HTTP_PORT", "private-value", "base-10"},
		{"port zero", "HTTP_PORT", "0", "between"},
		{"port negative", "HTTP_PORT", "-1", "between"},
		{"port excessive", "HTTP_PORT", "65536", "between"},
		{"port overflow", "HTTP_PORT", "999999999999999999999999", "base-10"},
		{"body malformed", "HTTP_MAX_BODY_BYTES", "1MiB", "base-10"},
		{"body zero", "HTTP_MAX_BODY_BYTES", "0", "between"},
		{"body negative", "HTTP_MAX_BODY_BYTES", "-1", "between"},
		{"body too small", "HTTP_MAX_BODY_BYTES", "1023", "between"},
		{"body excessive", "HTTP_MAX_BODY_BYTES", "16777217", "between"},
		{"body overflow", "HTTP_MAX_BODY_BYTES", "999999999999999999999999", "base-10"},
		{"read shorter than header", "HTTP_READ_TIMEOUT", "1s", "at least HTTP_READ_HEADER_TIMEOUT"},
	}
	keys := []string{"APP_ENV", "APP_NAME", "HTTP_HOST", "HTTP_PORT", "HTTP_MAX_BODY_BYTES"}
	for _, key := range []string{"HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT"} {
		keys = append(keys, key)
		for _, value := range []string{"private-value", "1", "999999999999999999999h", "0s", "-1s", "1ns", "16m"} {
			reason := "between"
			if value == "private-value" || value == "1" || strings.HasSuffix(value, "h") {
				reason = "valid Go duration"
			}
			tests = append(tests, struct{ name, key, value, reason string }{key + "/" + value, key, value, reason})
		}
	}
	for _, key := range keys {
		tests = append(tests, struct{ name, key, value, reason string }{key + "/empty", key, "", "must not be empty"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(lookup(map[string]string{tt.key: tt.value}))
			if err == nil || !strings.Contains(err.Error(), tt.key) || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("expected safe error for %s containing %q; got %v", tt.key, tt.reason, err)
			}
			if got != (config.Config{}) {
				t.Fatal("invalid configuration returned a partial snapshot")
			}
		})
	}
}

func TestMissingRequiredSettings(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		for _, key := range []string{"APP_NAME", "HTTP_HOST"} {
			t.Run(env+"/"+key, func(t *testing.T) {
				t.Parallel()
				values := map[string]string{"APP_ENV": env, "APP_NAME": "api", "HTTP_HOST": "127.0.0.1"}
				values["INSTANCE_ID"] = "api-1"
				delete(values, key)
				_, err := config.Load(lookup(values))
				if err == nil || err.Error() != "configuration: "+key+": is required" {
					t.Fatalf("unexpected missing-setting error: %v", err)
				}
				values[key] = ""
				_, err = config.Load(lookup(values))
				if err == nil || err.Error() != "configuration: "+key+": must not be empty" {
					t.Fatalf("empty setting was not distinguished: %v", err)
				}
			})
		}
	}
}

func TestResourceBoundaries(t *testing.T) {
	for _, value := range []string{"1", "65535"} {
		if _, err := config.Load(lookup(map[string]string{"HTTP_PORT": value})); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"1024", "16777216"} {
		if _, err := config.Load(lookup(map[string]string{"HTTP_MAX_BODY_BYTES": value})); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"1ms", "5m"} {
		values := map[string]string{}
		for _, key := range []string{"HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT"} {
			values[key] = value
		}
		if _, err := config.Load(lookup(values)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := config.Load(lookup(map[string]string{"HTTP_IDLE_TIMEOUT": "15m", "APP_NAME": strings.Repeat("a", 64)})); err != nil {
		t.Fatal(err)
	}
}

func TestSafeErrorFormatting(t *testing.T) {
	const sensitive = "PRIVATE_SENTINEL\nforged-log"
	for _, key := range []string{"APP_ENV", "APP_NAME", "HTTP_HOST", "HTTP_PORT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT", "HTTP_MAX_BODY_BYTES"} {
		t.Run(key, func(t *testing.T) {
			_, err := config.Load(lookup(map[string]string{key: sensitive}))
			if err == nil {
				t.Fatal("expected validation error")
			}
			for _, formatted := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				if strings.Contains(formatted, "PRIVATE_SENTINEL") || strings.ContainsAny(formatted, "\n\r") || !strings.Contains(formatted, key) {
					t.Fatal("error formatting leaked input or omitted setting name")
				}
			}
		})
	}
}

func TestDeterministicLoading(t *testing.T) {
	values := map[string]string{"APP_NAME": "first"}
	calls := map[string]int{}
	snapshot, err := config.Load(func(key string) (string, bool) {
		calls[key]++
		return lookup(values)(key)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 13 {
		t.Fatalf("expected exactly 13 settings read, got %d", len(calls))
	}
	for key, count := range calls {
		if count != 1 {
			t.Fatalf("%s read %d times", key, count)
		}
	}
	values["APP_NAME"] = "second"
	second, err := config.Load(lookup(values))
	if err != nil || snapshot.AppName != "first" || second.AppName != "second" {
		t.Fatal("load results share state or are not deterministic")
	}
	if _, err := config.Load(nil); err == nil {
		t.Fatal("nil lookup must fail explicitly")
	}
}
