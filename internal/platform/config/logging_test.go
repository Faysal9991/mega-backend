package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func TestLoggingConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  map[string]string
		want config.LoggingConfig
	}{
		{"defaults", nil, config.LoggingConfig{Level: slog.LevelInfo, Format: config.LogText, InstanceID: "local"}},
		{"debug JSON", map[string]string{"LOG_LEVEL": "DEBUG", "LOG_FORMAT": "json", "INSTANCE_ID": "api-1.a_b"}, config.LoggingConfig{Level: slog.LevelDebug, Format: config.LogJSON, InstanceID: "api-1.a_b"}},
		{"warn", map[string]string{"LOG_LEVEL": "WARN"}, config.LoggingConfig{Level: slog.LevelWarn, Format: config.LogText, InstanceID: "local"}},
		{"error", map[string]string{"LOG_LEVEL": "ERROR"}, config.LoggingConfig{Level: slog.LevelError, Format: config.LogText, InstanceID: "local"}},
		{"production default JSON", map[string]string{"APP_ENV": "production", "APP_NAME": "api", "HTTP_HOST": "127.0.0.1", "INSTANCE_ID": "api-1"}, config.LoggingConfig{Level: slog.LevelInfo, Format: config.LogJSON, InstanceID: "api-1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.Load(lookup(tt.env))
			if err != nil || got.Logging != tt.want {
				t.Fatalf("logging config = %+v, %v; want %+v", got.Logging, err, tt.want)
			}
		})
	}
}

func TestLoggingConfigurationInvalid(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"LOG_LEVEL", ""}, {"LOG_LEVEL", "info"}, {"LOG_LEVEL", "INFO+1"},
		{"LOG_FORMAT", ""}, {"LOG_FORMAT", "JSON"}, {"LOG_FORMAT", "yaml"},
		{"INSTANCE_ID", ""}, {"INSTANCE_ID", "has space"}, {"INSTANCE_ID", strings.Repeat("a", 65)},
		{"LOG_LEVEL", "PRIVATE_SENTINEL\n"}, {"LOG_FORMAT", "PRIVATE_SENTINEL\n"}, {"INSTANCE_ID", "PRIVATE_SENTINEL\n"},
	} {
		t.Run(tt.key+"/"+tt.value, func(t *testing.T) {
			got, err := config.Load(lookup(map[string]string{tt.key: tt.value}))
			if err == nil || !strings.Contains(err.Error(), tt.key) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") || got != (config.Config{}) {
				t.Fatalf("unsafe or missing error for %s: %v", tt.key, err)
			}
		})
	}
	for _, env := range []string{"staging", "production"} {
		values := map[string]string{"APP_ENV": env, "APP_NAME": "api", "HTTP_HOST": "127.0.0.1"}
		_, err := config.Load(lookup(values))
		if err == nil || err.Error() != "configuration: INSTANCE_ID: is required" {
			t.Fatalf("missing instance: %v", err)
		}
		values["INSTANCE_ID"] = ""
		_, err = config.Load(lookup(values))
		if err == nil || err.Error() != "configuration: INSTANCE_ID: must not be empty" {
			t.Fatalf("empty instance: %v", err)
		}
		values["INSTANCE_ID"] = "api-1"
		values["LOG_FORMAT"] = "text"
		_, err = config.Load(lookup(values))
		if err == nil || !strings.Contains(err.Error(), "LOG_FORMAT") {
			t.Fatal("production text must be rejected")
		}
	}
	if _, err := config.Load(lookup(map[string]string{"INSTANCE_ID": strings.Repeat("a", 64)})); err != nil {
		t.Fatal(err)
	}
}
